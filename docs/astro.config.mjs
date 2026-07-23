import { defineConfig } from "astro/config";
import starlight from "@astrojs/starlight";

// Published to GitHub Pages at https://wlix13.github.io/Orrery/, so `base`
// must match the repository name.
export default defineConfig({
  site: "https://wlix13.github.io",
  base: "/Orrery",
  integrations: [
    starlight({
      title: "Orrery",
      favicon: "/favicon.svg",
      description: "Xray metrics collection for the Conglomerate proxy fleet.",
      social: [{ icon: "github", label: "GitHub", href: "https://github.com/wlix13/Orrery" }],
      editLink: { baseUrl: "https://github.com/wlix13/Orrery/edit/main/docs/" },
      lastUpdated: true,
      sidebar: [
        { label: "Overview", link: "/" },
        {
          label: "Guides",
          items: [
            { label: "Getting started", link: "/guides/getting-started/" },
            { label: "Xray stats configuration", link: "/guides/xray-stats/" },
            { label: "Deployment and operations", link: "/guides/deployment/" },
            { label: "Troubleshooting", link: "/guides/troubleshooting/" },
          ],
        },
        {
          label: "Reference",
          items: [
            { label: "Configuration", link: "/reference/configuration/" },
            { label: "HTTP API", link: "/reference/api/" },
            { label: "Ecosystem", link: "/reference/ecosystem/" },
            { label: "Architecture", link: "/reference/architecture/" },
          ],
        },
      ],
    }),
  ],
});
