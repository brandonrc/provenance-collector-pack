package dashboard

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
)

// scanReq builds a same-origin POST to /api/scan with an optional bearer.
// All /api/scan tests must go through this so they pass the CSRF gate.
func scanReq(bearer string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/api/scan", nil)
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	return req
}

const (
	testNamespace    = "provenance-system"
	testCronName     = "provenance-collector"
	testManualJobTTL = 90 * time.Minute // distinct from DefaultManualJobTTL so tests fail loudly on a regression
)

func makeCronJob() *batchv1.CronJob {
	return &batchv1.CronJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testCronName,
			Namespace: testNamespace,
			UID:       types.UID("cj-uid-1"),
		},
		Spec: batchv1.CronJobSpec{
			JobTemplate: batchv1.JobTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"app.kubernetes.io/name": "provenance-collector"},
				},
				Spec: batchv1.JobSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							RestartPolicy: corev1.RestartPolicyOnFailure,
							Containers: []corev1.Container{{
								Name:  "collector",
								Image: "test/collector:latest",
							}},
						},
					},
				},
			},
		},
	}
}

func TestStartManualScan_CreatesJob(t *testing.T) {
	client := fake.NewClientset(makeCronJob())
	runner := NewK8sScanRunner(client, testNamespace, testCronName, testManualJobTTL)

	name, err := runner.StartManualScan(context.Background())
	if err != nil {
		t.Fatalf("StartManualScan: %v", err)
	}

	job, err := client.BatchV1().Jobs(testNamespace).Get(context.Background(), name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get created job: %v", err)
	}
	if job.Annotations["cronjob.kubernetes.io/instantiate"] != "manual" {
		t.Errorf("missing/incorrect instantiate annotation: %v", job.Annotations)
	}
	if len(job.OwnerReferences) != 1 {
		t.Fatalf("expected 1 owner reference, got %d", len(job.OwnerReferences))
	}
	owner := job.OwnerReferences[0]
	if owner.Kind != "CronJob" || owner.Name != testCronName || owner.UID != "cj-uid-1" {
		t.Errorf("unexpected owner reference: %+v", owner)
	}
	if len(job.Spec.Template.Spec.Containers) != 1 || job.Spec.Template.Spec.Containers[0].Image != "test/collector:latest" {
		t.Errorf("job did not inherit CronJob template containers: %+v", job.Spec.Template.Spec.Containers)
	}
	if job.Spec.TTLSecondsAfterFinished == nil {
		t.Fatal("expected TTLSecondsAfterFinished to be set on a manual Job")
	}
	if got := *job.Spec.TTLSecondsAfterFinished; got != int32(testManualJobTTL.Seconds()) {
		t.Errorf("expected TTLSecondsAfterFinished=%d, got %d", int32(testManualJobTTL.Seconds()), got)
	}
}

func TestStartManualScan_TTLDisabled(t *testing.T) {
	client := fake.NewClientset(makeCronJob())
	runner := NewK8sScanRunner(client, testNamespace, testCronName, 0) // 0 = no TTL

	name, err := runner.StartManualScan(context.Background())
	if err != nil {
		t.Fatalf("StartManualScan: %v", err)
	}
	job, err := client.BatchV1().Jobs(testNamespace).Get(context.Background(), name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get created job: %v", err)
	}
	if job.Spec.TTLSecondsAfterFinished != nil {
		t.Errorf("expected TTLSecondsAfterFinished to be unset when manualJobTTL=0, got %d", *job.Spec.TTLSecondsAfterFinished)
	}
}

func TestHasActiveManualJob(t *testing.T) {
	yes := true
	activeJob := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "manual-100",
			Namespace: testNamespace,
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: "batch/v1", Kind: "CronJob", Name: testCronName, UID: "cj-uid-1",
				Controller: &yes,
			}},
		},
		Status: batchv1.JobStatus{Active: 1},
	}
	completedJob := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "manual-99",
			Namespace: testNamespace,
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: "batch/v1", Kind: "CronJob", Name: testCronName, UID: "cj-uid-1",
				Controller: &yes,
			}},
		},
		Status: batchv1.JobStatus{
			Succeeded:      1,
			CompletionTime: &metav1.Time{},
			Conditions: []batchv1.JobCondition{
				{Type: batchv1.JobComplete, Status: corev1.ConditionTrue},
			},
		},
	}
	failedJob := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "manual-98",
			Namespace: testNamespace,
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: "batch/v1", Kind: "CronJob", Name: testCronName, UID: "cj-uid-1",
				Controller: &yes,
			}},
		},
		// Mirrors what the Job controller writes when backoffLimit is
		// exhausted: Failed > 0, no CompletionTime, but a Failed condition.
		Status: batchv1.JobStatus{
			Failed: 3,
			Conditions: []batchv1.JobCondition{
				{Type: batchv1.JobFailed, Status: corev1.ConditionTrue},
			},
		},
	}

	t.Run("active", func(t *testing.T) {
		c := fake.NewClientset(makeCronJob(), activeJob)
		runner := NewK8sScanRunner(c, testNamespace, testCronName, testManualJobTTL)
		got, err := runner.HasActiveManualJob(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if !got {
			t.Error("expected HasActiveManualJob=true with Active=1 job present")
		}
	})

	t.Run("only completed", func(t *testing.T) {
		c := fake.NewClientset(makeCronJob(), completedJob)
		runner := NewK8sScanRunner(c, testNamespace, testCronName, testManualJobTTL)
		got, err := runner.HasActiveManualJob(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if got {
			t.Error("expected HasActiveManualJob=false when only completed job exists")
		}
	})

	t.Run("only failed", func(t *testing.T) {
		c := fake.NewClientset(makeCronJob(), failedJob)
		runner := NewK8sScanRunner(c, testNamespace, testCronName, testManualJobTTL)
		got, err := runner.HasActiveManualJob(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if got {
			t.Error("expected HasActiveManualJob=false when only hard-failed job exists (regression guard for the heuristic that ignored Failed conditions)")
		}
	})

	t.Run("ignores foreign cronjob", func(t *testing.T) {
		foreign := activeJob.DeepCopy()
		foreign.Name = "manual-other"
		foreign.OwnerReferences[0].Name = "some-other-cronjob"
		c := fake.NewClientset(makeCronJob(), foreign)
		runner := NewK8sScanRunner(c, testNamespace, testCronName, testManualJobTTL)
		got, err := runner.HasActiveManualJob(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if got {
			t.Error("HasActiveManualJob should ignore jobs owned by a different CronJob")
		}
	})
}

// fakeRunner is a stub ScanRunner for HTTP-layer tests.
type fakeRunner struct {
	active    bool
	activeErr error
	startName string
	startErr  error
	starts    int
}

func (f *fakeRunner) HasActiveManualJob(context.Context) (bool, error) {
	return f.active, f.activeErr
}
func (f *fakeRunner) StartManualScan(context.Context) (string, error) {
	f.starts++
	return f.startName, f.startErr
}

func newAdminSrv(t *testing.T, runner ScanRunner) *Server {
	t.Helper()
	stub := userInfoStub(t, map[string]string{
		"admin": `{"sub":"u1","email":"a@x","groups":["/admins"]}`,
		"user":  `{"sub":"u2","email":"u@x","groups":["/users"]}`,
	}, nil)
	t.Cleanup(stub.Close)
	return NewServer(t.TempDir()).
		WithAuth(AuthConfig{IssuerURL: stub.URL, AdminGroups: []string{"/admins"}}).
		WithScanRunner(runner)
}

func TestScan_RequiresPOST(t *testing.T) {
	srv := newAdminSrv(t, &fakeRunner{startName: "manual-1"})
	req := httptest.NewRequest(http.MethodGet, "/api/scan", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", w.Code)
	}
}

func TestScan_AuthDisabled(t *testing.T) {
	srv := NewServer(t.TempDir()) // no auth wired
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, scanReq(""))
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503 with no auth configured, got %d", w.Code)
	}
}

func TestScan_NoRunner(t *testing.T) {
	stub := userInfoStub(t, map[string]string{
		"admin": `{"groups":["/admins"]}`,
	}, nil)
	defer stub.Close()
	srv := NewServer(t.TempDir()).
		WithAuth(AuthConfig{IssuerURL: stub.URL, AdminGroups: []string{"/admins"}})
	// note: no WithScanRunner

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, scanReq("admin"))
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503 with no runner wired, got %d", w.Code)
	}
}

func TestScan_Forbidden(t *testing.T) {
	runner := &fakeRunner{startName: "manual-1"}
	srv := newAdminSrv(t, runner)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, scanReq("user")) // non-admin token
	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403 for non-admin, got %d", w.Code)
	}
	if runner.starts != 0 {
		t.Errorf("non-admin must not trigger StartManualScan, got %d calls", runner.starts)
	}
}

func TestScan_NoBearer(t *testing.T) {
	srv := newAdminSrv(t, &fakeRunner{startName: "manual-1"})
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, scanReq(""))
	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403 when no bearer is supplied, got %d", w.Code)
	}
}

func TestScan_CSRF(t *testing.T) {
	srv := newAdminSrv(t, &fakeRunner{startName: "manual-1"})

	cases := map[string]string{
		"missing":    "",
		"cross-site": "cross-site",
		"same-site":  "same-site",
		"none":       "none",
	}
	for name, site := range cases {
		t.Run(name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/scan", nil)
			if site != "" {
				req.Header.Set("Sec-Fetch-Site", site)
			}
			// Use a valid admin bearer so we know the rejection is from the
			// CSRF gate and not from a missing identity.
			req.Header.Set("Authorization", "Bearer admin")
			w := httptest.NewRecorder()
			srv.ServeHTTP(w, req)
			if w.Code != http.StatusForbidden {
				t.Errorf("expected 403 for Sec-Fetch-Site=%q, got %d", site, w.Code)
			}
			if !strings.Contains(w.Body.String(), "cross-origin") {
				t.Errorf("expected CSRF rejection body, got %q", w.Body.String())
			}
		})
	}
}

func TestScan_Conflict(t *testing.T) {
	runner := &fakeRunner{active: true, startName: "manual-1"}
	srv := newAdminSrv(t, runner)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, scanReq("admin"))
	if w.Code != http.StatusConflict {
		t.Errorf("expected 409 when a job is already active, got %d", w.Code)
	}
	if runner.starts != 0 {
		t.Errorf("must not start a scan when one is already active; got %d starts", runner.starts)
	}
}

func TestScan_Success(t *testing.T) {
	runner := &fakeRunner{startName: "manual-42"}
	srv := newAdminSrv(t, runner)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, scanReq("admin"))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	var resp scanResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("bad json: %v", err)
	}
	if resp.JobName != "manual-42" {
		t.Errorf("expected jobName=manual-42, got %q", resp.JobName)
	}
	if runner.starts != 1 {
		t.Errorf("expected exactly one StartManualScan call, got %d", runner.starts)
	}
}
