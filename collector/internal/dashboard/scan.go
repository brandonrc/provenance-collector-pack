package dashboard

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// ScanRunner abstracts the side effects of /api/scan so tests can swap in a
// fake without standing up an apiserver.
type ScanRunner interface {
	// HasActiveManualJob reports whether a Job owned by the watched CronJob
	// is currently active (not yet completed or failed).
	HasActiveManualJob(ctx context.Context) (bool, error)
	// StartManualScan creates a new one-shot Job from the watched CronJob's
	// jobTemplate and returns the new Job's name.
	StartManualScan(ctx context.Context) (string, error)
}

// DefaultManualJobTTL is the fallback TTL applied to manual Jobs when no
// explicit value is configured. The report itself is the durable artifact
// (stored on the dashboard's PVC with its own retention); the Job object
// only needs to live long enough for an operator to inspect pod logs after
// a failure.
const DefaultManualJobTTL = time.Hour

// k8sScanRunner is the production ScanRunner: it talks to a real apiserver
// via client-go to inspect and create Jobs from a specific CronJob.
type k8sScanRunner struct {
	client       kubernetes.Interface
	namespace    string
	cronJob      string
	manualJobTTL time.Duration // 0 disables TTL on manual Jobs entirely
}

// NewK8sScanRunner builds a ScanRunner backed by the provided clientset.
// Exported so cmd/dashboard can wire it from an in-cluster config.
// manualJobTTL controls Spec.TTLSecondsAfterFinished on created Jobs;
// pass 0 to skip setting the field (Job persists until namespace teardown
// or manual deletion).
func NewK8sScanRunner(client kubernetes.Interface, namespace, cronJob string, manualJobTTL time.Duration) ScanRunner {
	return &k8sScanRunner{
		client:       client,
		namespace:    namespace,
		cronJob:      cronJob,
		manualJobTTL: manualJobTTL,
	}
}

func (k *k8sScanRunner) HasActiveManualJob(ctx context.Context) (bool, error) {
	jobs, err := k.client.BatchV1().Jobs(k.namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return false, err
	}
	for _, j := range jobs.Items {
		if !ownedBy(j.OwnerReferences, "CronJob", k.cronJob) {
			continue
		}
		if !jobDone(j.Status.Conditions) {
			return true, nil
		}
	}
	return false, nil
}

// jobDone reports whether a Job has reached a terminal state. The Job
// controller writes a Complete or Failed condition exactly once when the
// Job stops progressing; checking conditions is more reliable than
// inspecting Active/Succeeded/Failed counters, which all read zero in the
// brief window between the last pod being collected and the condition being
// written (and, post-Kubernetes 1.28, CompletionTime is only set on success).
func jobDone(conds []batchv1.JobCondition) bool {
	for _, c := range conds {
		if c.Status != corev1.ConditionTrue {
			continue
		}
		if c.Type == batchv1.JobComplete || c.Type == batchv1.JobFailed {
			return true
		}
	}
	return false
}

func (k *k8sScanRunner) StartManualScan(ctx context.Context) (string, error) {
	cj, err := k.client.BatchV1().CronJobs(k.namespace).Get(ctx, k.cronJob, metav1.GetOptions{})
	if err != nil {
		return "", err
	}
	yes := true
	spec := cj.Spec.JobTemplate.Spec
	// Scheduled runs are pruned by successfulJobsHistoryLimit, but manual
	// runs aren't tracked there and would otherwise pile up indefinitely.
	// Override whatever TTL the CronJob's jobTemplate may carry (operators
	// who want the Jobs to stick around can set manualJobTTL=0).
	if k.manualJobTTL > 0 {
		ttl := int32(k.manualJobTTL.Seconds())
		spec.TTLSecondsAfterFinished = &ttl
	}
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			// GenerateName lets the apiserver pick a unique suffix so two
			// admin clicks within the same TOCTOU window cannot collide on
			// the same Job name.
			GenerateName: "manual-",
			Namespace:    k.namespace,
			Labels:       cj.Spec.JobTemplate.Labels,
			Annotations: map[string]string{
				// Same annotation `kubectl create job --from=cronjob/...` sets.
				"cronjob.kubernetes.io/instantiate": "manual",
			},
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion:         "batch/v1",
				Kind:               "CronJob",
				Name:               cj.Name,
				UID:                cj.UID,
				Controller:         &yes,
				BlockOwnerDeletion: &yes,
			}},
		},
		Spec: spec,
	}
	created, err := k.client.BatchV1().Jobs(k.namespace).Create(ctx, job, metav1.CreateOptions{})
	if err != nil {
		return "", err
	}
	return created.Name, nil
}

func ownedBy(refs []metav1.OwnerReference, kind, name string) bool {
	for _, o := range refs {
		if o.Kind == kind && o.Name == name {
			return true
		}
	}
	return false
}

// scanResponse is the JSON shape returned by a successful POST /api/scan.
type scanResponse struct {
	JobName   string `json:"jobName"`
	Namespace string `json:"namespace"`
}

func (s *Server) handleScan(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	// CSRF defense. With forwardAccessToken on, the gateway injects the
	// bearer from the user's session cookie on every upstream request, so a
	// cross-origin form POST landing here is already authenticated.
	// Sec-Fetch-Site is on the browser's forbidden-header list (sites can't
	// forge it via fetch()/XHR), so requiring `same-origin` blocks the
	// cross-site case. Fail closed when the header is missing: old browsers
	// and curl-style clients don't send it, and this endpoint is only meant
	// to be triggered from the dashboard UI.
	if r.Header.Get("Sec-Fetch-Site") != "same-origin" {
		http.Error(w, "cross-origin requests not allowed", http.StatusForbidden)
		return
	}
	if !s.auth.enabled() {
		http.Error(w, "scan endpoint is not configured (oidcIssuer/adminGroups unset)", http.StatusServiceUnavailable)
		return
	}
	id := s.auth.identify(r.Context(), r)
	if !s.auth.canRunScan(id) {
		http.Error(w, "forbidden: caller is not in an admin group", http.StatusForbidden)
		return
	}
	if s.scan == nil {
		http.Error(w, "scan endpoint is not configured (kubernetes client unavailable)", http.StatusServiceUnavailable)
		return
	}

	active, err := s.scan.HasActiveManualJob(r.Context())
	if err != nil {
		http.Error(w, "failed to inspect running jobs: "+err.Error(), http.StatusBadGateway)
		return
	}
	if active {
		http.Error(w, "a scan job is already active for this collector", http.StatusConflict)
		return
	}

	name, err := s.scan.StartManualScan(r.Context())
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			http.Error(w, "scan request canceled", http.StatusGatewayTimeout)
			return
		}
		http.Error(w, "failed to start scan: "+err.Error(), http.StatusBadGateway)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(scanResponse{
		JobName:   name,
		Namespace: namespaceOf(s.scan),
	})
}

// namespaceOf surfaces the namespace from a ScanRunner for the response body
// without leaking the concrete type. Returns "" for runners that don't expose
// one (e.g. test fakes that don't need it).
func namespaceOf(r ScanRunner) string {
	if k, ok := r.(*k8sScanRunner); ok {
		return k.namespace
	}
	if n, ok := r.(interface{ Namespace() string }); ok {
		return n.Namespace()
	}
	return ""
}
