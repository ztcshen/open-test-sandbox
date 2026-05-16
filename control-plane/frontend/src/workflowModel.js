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

export function dashboardStatusById(dashboard) {
  const byId = new Map();
  (dashboard?.groups || []).forEach((group) => {
    (group.items || []).forEach((item) => byId.set(item.id, item));
  });
  return byId;
}

export function workflowServiceSearchText(workflow, services) {
  const serviceById = new Map((services || []).map((service) => [service.id, service]));
  return workflowServiceIds(workflow)
    .map((serviceId) => {
      const service = serviceById.get(serviceId);
      return service ? [service.id, service.displayName, service.role].filter(Boolean).join(" ") : `${serviceId} 未建模`;
    })
    .join(" ");
}

export function workflowRuntimeImpact(workflow, statusById = new Map()) {
  const runtimeItems = workflowServiceIds(workflow).map((serviceId) => statusById.get(serviceId)).filter(Boolean);
  const badCount = runtimeItems.filter((item) => !item.ok).length;
  if (!runtimeItems.length) {
    return { text: "运行态未覆盖", tone: "unknown" };
  }
  return badCount ? { text: `${badCount} 异常服务`, tone: "bad" } : { text: "服务正常", tone: "ok" };
}

export function filterWorkflows(workflows, services, query, statusById = new Map()) {
  const text = String(query || "").trim().toLowerCase();
  if (!text) return workflows;
  return (workflows || []).filter((workflow) => {
    const stepText = (workflow.steps || [])
      .map((step) => [step.id, step.displayName, step.caseId, step.serviceId, step.action, ...(step.evidenceKinds || [])].filter(Boolean).join(" "))
      .join(" ");
    const serviceText = workflowServiceSearchText(workflow, services);
    const impactText = workflowRuntimeImpact(workflow, statusById).text;
    return [
      workflow.id,
      workflow.displayName,
      workflow.description,
      workflow.entrypoint,
      workflowKindLabel(workflow),
      stepText,
      serviceText,
      impactText,
    ].filter(Boolean).join(" ").toLowerCase().includes(text);
  });
}
