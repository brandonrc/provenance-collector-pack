// docs/astro.config.mjs
import { defineConfig } from 'astro/config';
import starlight from '@astrojs/starlight';
import { nebari } from '@nebari/starlight';
import rehypeMermaid from 'rehype-mermaid';
import remarkBaseLinks from './src/plugins/remark-base-links';

// BASE and SITE are set by CI when deploying under a subpath
// (e.g. packs.nebari.dev/provenance-collector-pack/). Default '/' is the
// right thing for `astro dev` and local previews.
export default defineConfig({
  base: process.env.BASE || '/',
  site: process.env.SITE,
  integrations: [
    starlight({
      title: 'Security Posture',
      description:
        'Admin-only security posture for Nebari clusters: supply-chain provenance from the Go provenance collector, Trivy + Grype + Clair vulnerability consensus, Kubernetes STIG posture checks, live NIST SP 800-53 control evidence, and POA&M / STIG / SAR / OSCAL reports.',
      // Shared Nebari identity (brand colors, fonts, logo, favicon, footer, GitHub link)
      // comes from the @nebari/starlight theme plugin. logoHref sends the header logo
      // to the pack catalog; githubHref points the GitHub icon at this repository.
      plugins: [
        nebari({
          logoHref: 'https://packs.nebari.dev/',
          githubHref: 'https://github.com/nebari-dev/provenance-collector-pack',
        }),
      ],
      lastUpdated: true,
      sidebar: [
        {
          label: 'Overview',
          items: [{ label: 'Introduction', slug: 'index' }],
        },
        {
          label: 'Guides',
          items: [
            { label: 'Quick Start', slug: 'quick-start' },
            { label: 'Migrating from 0.1.x', slug: 'migrating' },
            { label: 'Architecture', slug: 'architecture' },
            { label: 'Web UI', slug: 'web-dashboard' },
            { label: 'Storage', slug: 'storage-modes' },
          ],
        },
        {
          label: 'Compliance',
          items: [
            { label: 'Compliance architecture', slug: 'compliance-architecture' },
            { label: 'Controls', slug: 'controls' },
            { label: 'Reports', slug: 'reports' },
            { label: 'Supply-chain provenance', slug: 'provenance' },
            { label: 'Scoring', slug: 'scoring' },
          ],
        },
        {
          label: 'Reference',
          items: [
            { label: 'Configuration', slug: 'configuration' },
            { label: 'Collector environment', slug: 'collector-environment' },
            { label: 'Report Schema', slug: 'report-schema' },
            { label: 'NebariApp CRD', slug: 'nebariapp-crd-reference' },
            { label: 'Verifying Images', slug: 'verifying-images' },
          ],
        },
        {
          label: 'Design',
          items: [
            { label: 'Design', slug: 'design' },
            { label: 'Decisions', slug: 'decisions' },
            { label: 'Proposal 0001: merge', slug: 'proposals/0001-merge' },
          ],
        },
      ],
    }),
  ],
  markdown: {
    // Turn Shiki off for mermaid so rehype-mermaid sees the raw graph source.
    syntaxHighlight: { type: 'shiki', excludeLangs: ['mermaid'] },
    remarkPlugins: [[remarkBaseLinks, { base: process.env.BASE || '/' }]],
    rehypePlugins: [[rehypeMermaid, { strategy: 'inline-svg' }]],
  },
});
