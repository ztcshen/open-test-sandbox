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
    agentRun: resolve(root, "src/agentRun.jsx"),
    apiCases: resolve(root, "src/apiCases.jsx"),
    caseRuns: resolve(root, "src/caseRuns.jsx"),
    dashboard: resolve(root, "src/dashboard.jsx"),
    environmentNode: resolve(root, "src/environmentNode.jsx"),
    environmentNodes: resolve(root, "src/environmentNodes.jsx"),
    evidenceViewer: resolve(root, "src/evidenceViewer.jsx"),
    interfaceNode: resolve(root, "src/interfaceNode.jsx"),
    interfaceNodes: resolve(root, "src/interfaceNodes.jsx"),
    replayEvidence: resolve(root, "src/replayEvidence.jsx"),
    sandboxWorkbench: resolve(root, "src/sandboxWorkbench.jsx"),
    serviceInventory: resolve(root, "src/serviceInventory.jsx"),
    traceTopology: resolve(root, "src/traceTopology.jsx"),
    workflowDetail: resolve(root, "src/workflowDetail.jsx"),
    workflowBlueprintDemo: resolve(root, "src/workflowBlueprintDemo.jsx"),
    workflowRun: resolve(root, "src/workflowRun.jsx"),
    workflowStep: resolve(root, "src/workflowStep.jsx"),
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
