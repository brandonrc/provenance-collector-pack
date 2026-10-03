// docs/astro.config.mjs
import { defineConfig } from 'astro/config';
import starlight from '@astrojs/starlight';
import { nebari } from '@nebari/starlight';
import rehypeMermaid from 'rehype-mermaid';
import remarkBaseLinks from './src/plugins/remark-base-links';

// BASE and SITE are set by CI when deploying under a subpath
// (e.g. nebari-dev.github.io/provenance-collector-pack/). Default '/' is the
// right thing for `astro dev` and local previews.
export default defineConfig({
  base: process.env.BASE || '/',
  site: process.env.SITE,
  integrations: [
    starlight({
      title: 'Provenance Collector',
      description:
        'Compliance-grade supply-chain provenance for every container image and Helm release running on a Nebari cluster. A Kubernetes-native CronJob that discovers running images, resolves digests, verifies signatures, detects SLSA provenance and SBOM attestations, and emits a timestamped JSON report.',
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
          items: [
            { label: 'Introduction', slug: 'index' },
          ],
        },
        {
          label: 'Guides',
          items: [
            { label: 'Quick Start', slug: 'quick-start' },
            { label: 'Architecture', slug: 'architecture' },
            { label: 'Storage Modes', slug: 'storage-modes' },
            { label: 'Web Dashboard', slug: 'web-dashboard' },
          ],
        },
        {
          label: 'Reference',
          items: [
            { label: 'Configuration', slug: 'configuration' },
            { label: 'Report Schema', slug: 'report-schema' },
            { label: 'Consuming the Report', slug: 'consuming-the-report' },
            { label: 'NebariApp CRD', slug: 'nebariapp-crd-reference' },
            { label: 'Verifying Images', slug: 'verifying-images' },
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
