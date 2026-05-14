import { copyFileSync, rmSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { build } from "esbuild";

const root = dirname(fileURLToPath(import.meta.url));
const outdir = resolve(root, "../static/assets/react");

rmSync(outdir, { recursive: true, force: true });

await build({
  entryPoints: {
    controlPlane: resolve(root, "src/controlPlane.jsx"),
    agentTest: resolve(root, "src/agentTest.jsx"),
    apiCases: resolve(root, "src/apiCases.jsx"),
    caseRuns: resolve(root, "src/caseRuns.jsx"),
    dashboard: resolve(root, "src/dashboard.jsx"),
    workflowBlueprintDemo: resolve(root, "src/workflowBlueprintDemo.jsx"),
    workflows: resolve(root, "src/workflows.jsx"),
  },
  bundle: true,
  splitting: true,
  format: "esm",
  target: ["es2022"],
  jsx: "automatic",
  minify: true,
  treeShaking: true,
  define: {
    "process.env.NODE_ENV": "\"production\"",
  },
  outdir,
  entryNames: "[name]",
  chunkNames: "chunks/[name]-[hash]",
  assetNames: "[name]",
  loader: {
    ".css": "css",
  },
  logLevel: "info",
});

copyFileSync(resolve(root, "src/controlPlane.css"), resolve(outdir, "controlPlane.css"));
