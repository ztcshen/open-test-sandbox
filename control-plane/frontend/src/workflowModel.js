import { unique } from "./api.js";

export function workflowKind(workflow) {
  if (workflow?.presentation?.kind) {
    return workflow.presentation.kind;
  }
  return String(workflow?.entrypoint || "").includes("workflow-studio.html")
    ? "businessFlow"
    : "controlPlaneTool";
}

export function workflowKindLabel(workflow) {
  return workflowKind(workflow) === "businessFlow" ? "业务流" : "观测/工具";
}

export function workflowActionLabel(workflow) {
  return workflowKind(workflow) === "businessFlow" ? "打开 Workflow" : "打开入口";
}

export function workflowEntrypointHref(workflow) {
  const entrypoint = workflow?.entrypoint || "/workflow-studio.html";
  if (!workflow?.id || !entrypoint.startsWith("/")) {
    return entrypoint;
  }
  const url = new URL(entrypoint, window.location.origin);
  if (url.pathname.endsWith("/workflow-studio.html") && !url.searchParams.has("workflow")) {
    url.searchParams.set("workflow", workflow.id);
  }
  return `${url.pathname}${url.search}${url.hash}`;
}

export function workflowStepHref(workflowId, stepId) {
  return `/workflow-step.html?workflow=${encodeURIComponent(workflowId || "")}&step=${encodeURIComponent(stepId || "")}`;
}

export function workflowServiceIds(workflow) {
  return unique((workflow?.steps || []).map((step) => step.serviceId));
}

export function filterWorkflows(workflows, services, query) {
  const text = String(query || "").trim().toLowerCase();
  if (!text) return workflows;
  const serviceById = new Map((services || []).map((service) => [service.id, service]));
  return (workflows || []).filter((workflow) => {
    const stepText = (workflow.steps || [])
      .map((step) => [step.id, step.displayName, step.caseId, step.serviceId, step.action].filter(Boolean).join(" "))
      .join(" ");
    const serviceText = workflowServiceIds(workflow)
      .map((serviceId) => {
        const service = serviceById.get(serviceId);
        return service ? [service.id, service.displayName, service.role].filter(Boolean).join(" ") : `${serviceId} 未建模`;
      })
      .join(" ");
    return [
      workflow.id,
      workflow.displayName,
      workflow.description,
      workflow.entrypoint,
      workflowKindLabel(workflow),
      stepText,
      serviceText,
    ].filter(Boolean).join(" ").toLowerCase().includes(text);
  });
}
