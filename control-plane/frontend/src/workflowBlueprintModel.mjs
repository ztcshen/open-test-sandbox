export const nodeTemplates = [
  {
    type: "API_STEP",
    name: "接口步骤",
    service: "service",
    inputs: [{ id: "trigger", name: "触发", direction: "IN", portType: "EXEC" }],
    outputs: [
      { id: "next", name: "下一接口", direction: "OUT", portType: "EXEC", required: true },
      { id: "response", name: "接口响应", direction: "OUT", portType: "DATA", dataType: "object" },
      { id: "primaryKey", name: "关联主键", direction: "OUT", portType: "DATA", dataType: "key" },
    ],
    defaultConfig: {
      displayName: "接口步骤",
      serviceId: "service",
      caseId: "",
      operation: "",
      requestTemplate: "待关联 API Case",
      primaryKeyBinding: "待确认",
      aiConfirmation: "确认请求参数模板、接口输出字段、关联主键和断言证据",
    },
  },
  {
    type: "CALLBACK_STEP",
    name: "回调步骤",
    service: "callback-service",
    inputs: [
      { id: "trigger", name: "触发", direction: "IN", portType: "EXEC", required: true },
      { id: "correlationKey", name: "关联主键", direction: "IN", portType: "DATA", dataType: "key", required: true },
    ],
    outputs: [
      { id: "next", name: "回调完成", direction: "OUT", portType: "EXEC" },
      { id: "callbackResult", name: "回调结果", direction: "OUT", portType: "DATA", dataType: "object" },
    ],
    defaultConfig: {
      displayName: "回调步骤",
      serviceId: "callback-service",
      caseId: "",
      operation: "",
      requestTemplate: "待关联回调模板",
      primaryKeyBinding: "待确认",
      aiConfirmation: "确认回调请求模板、签名字段和关联键",
    },
  },
  {
    type: "QUERY_STEP",
    name: "查询步骤",
    service: "service",
    inputs: [
      { id: "trigger", name: "触发", direction: "IN", portType: "EXEC", required: true },
      { id: "queryKey", name: "查询主键", direction: "IN", portType: "DATA", dataType: "key", required: true },
    ],
    outputs: [{ id: "response", name: "查询响应", direction: "OUT", portType: "DATA", dataType: "object" }],
    defaultConfig: {
      displayName: "查询步骤",
      serviceId: "service",
      caseId: "",
      operation: "",
      requestTemplate: "待关联查询模板",
      primaryKeyBinding: "待确认",
      aiConfirmation: "确认查询请求模板、查询主键、期望状态和证据断言",
    },
  },
  {
    type: "ASSERT_STEP",
    name: "校验步骤",
    service: "control-plane",
    inputs: [
      { id: "trigger", name: "触发", direction: "IN", portType: "EXEC", required: true },
      { id: "evidence", name: "证据输入", direction: "IN", portType: "DATA", dataType: "object" },
    ],
    outputs: [{ id: "next", name: "校验通过", direction: "OUT", portType: "EXEC" }],
    defaultConfig: {
      displayName: "校验步骤",
      serviceId: "control-plane",
      caseId: "",
      operation: "assert",
      requestTemplate: "待确认",
      primaryKeyBinding: "不适用",
      aiConfirmation: "确认断言字段、证据来源和失败提示",
    },
  },
];

export const atomicInterfaceCatalog = [
  {
    id: "interface.prepare",
    domain: "通用流程",
    scenario: "创建与查询",
    serviceId: "service.alpha",
    actionType: "准备",
    name: "准备请求",
    templateType: "API_STEP",
    caseId: "case.prepare",
    operation: "item.prepare",
    primaryKeyBinding: "由 AI 确认准备响应中可传递给后续接口的主键",
    aiConfirmation: "确认请求参数模板、默认参数、响应输出字段和后续映射",
    requestParams: [
      { id: "tenantId", name: "租户标识", path: "$.request.tenantId", dataType: "string", required: true },
      { id: "itemId", name: "对象标识", path: "$.request.itemId", dataType: "string", required: true },
      { id: "amount", name: "金额", path: "$.request.amount", dataType: "number" },
      { id: "operatorId", name: "操作人", path: "$.request.operatorId", dataType: "string" },
    ],
    responseFields: [
      { id: "requestId", name: "请求标识", path: "$.response.requestId", dataType: "string", required: true },
      { id: "itemId", name: "对象标识", path: "$.response.itemId", dataType: "string", required: true },
      { id: "status", name: "状态", path: "$.response.status", dataType: "string" },
      { id: "expiresAt", name: "过期时间", path: "$.response.expiresAt", dataType: "datetime" },
    ],
  },
  {
    id: "interface.submit",
    domain: "通用流程",
    scenario: "创建与查询",
    serviceId: "service.alpha",
    actionType: "提交",
    name: "提交对象",
    templateType: "API_STEP",
    caseId: "case.submit",
    operation: "item.submit",
    primaryKeyBinding: "由 AI 确认提交响应或证据中的关联主键",
    aiConfirmation: "确认请求参数模板、上游输出映射、关联主键和断言证据",
    requestParams: [
      { id: "tenantId", name: "租户标识", path: "$.request.tenantId", dataType: "string", required: true },
      { id: "itemId", name: "对象标识", path: "$.request.itemId", dataType: "string", required: true },
      { id: "requestId", name: "请求标识", path: "$.request.requestId", dataType: "string" },
      { id: "payloadVersion", name: "载荷版本", path: "$.request.payloadVersion", dataType: "string" },
    ],
    responseFields: [
      { id: "submissionId", name: "提交标识", path: "$.response.submissionId", dataType: "string", required: true },
      { id: "itemId", name: "对象标识", path: "$.response.itemId", dataType: "string", required: true },
      { id: "status", name: "状态", path: "$.response.status", dataType: "string", required: true },
    ],
  },
  {
    id: "interface.callback",
    domain: "通用流程",
    scenario: "创建与查询",
    serviceId: "service.callback",
    actionType: "回调",
    name: "结果回调",
    templateType: "CALLBACK_STEP",
    caseId: "case.callback",
    operation: "item.callback",
    primaryKeyBinding: "用提交接口输出主键或外部流水号关联合成回调",
    aiConfirmation: "确认回调模板、签名字段、关联键和通知结果断言",
    requestParams: [
      { id: "submissionId", name: "提交标识", path: "$.request.submissionId", dataType: "string", required: true },
      { id: "callbackStatus", name: "回调状态", path: "$.request.callbackStatus", dataType: "string", required: true },
      { id: "signature", name: "签名", path: "$.request.signature", dataType: "string", required: true },
    ],
    responseFields: [
      { id: "notifyAccepted", name: "通知受理", path: "$.response.notifyAccepted", dataType: "boolean" },
      { id: "traceId", name: "Trace ID", path: "$.response.traceId", dataType: "string" },
    ],
  },
  {
    id: "interface.query",
    domain: "通用流程",
    scenario: "创建与查询",
    serviceId: "service.alpha",
    actionType: "查询",
    name: "结果查询",
    templateType: "QUERY_STEP",
    caseId: "case.query",
    operation: "item.query",
    primaryKeyBinding: "使用提交接口输出主键或外部流水号查询结果",
    aiConfirmation: "确认查询模板、查询主键、期望状态和证据断言",
    requestParams: [
      { id: "submissionId", name: "提交标识", path: "$.request.submissionId", dataType: "string" },
      { id: "itemId", name: "对象标识", path: "$.request.itemId", dataType: "string" },
    ],
    responseFields: [
      { id: "resultStatus", name: "结果状态", path: "$.response.resultStatus", dataType: "string", required: true },
      { id: "failReason", name: "失败原因", path: "$.response.failReason", dataType: "string" },
    ],
  },
  {
    id: "interface.archive",
    domain: "通用流程",
    scenario: "归档处理",
    serviceId: "service.alpha",
    actionType: "归档",
    name: "归档对象",
    templateType: "API_STEP",
    caseId: "case.archive",
    operation: "item.archive",
    primaryKeyBinding: "由 AI 确认归档请求关联主键",
    aiConfirmation: "确认请求参数模板、处理主体、关联主键和断言证据",
    requestParams: [
      { id: "itemId", name: "对象标识", path: "$.request.itemId", dataType: "string", required: true },
      { id: "archiveReason", name: "归档原因", path: "$.request.archiveReason", dataType: "string" },
    ],
    responseFields: [
      { id: "archiveStatus", name: "归档状态", path: "$.response.archiveStatus", dataType: "string" },
      { id: "archiveTraceNo", name: "归档流水号", path: "$.response.archiveTraceNo", dataType: "string" },
    ],
  },
];

const initialNodeSpecs = [
  { id: "api-1", templateType: "API_STEP", position: { x: 40, y: 130 } },
  { id: "callback-1", templateType: "CALLBACK_STEP", position: { x: 350, y: 80 } },
  { id: "query-1", templateType: "QUERY_STEP", position: { x: 350, y: 280 } },
  { id: "assert-1", templateType: "ASSERT_STEP", position: { x: 660, y: 180 } },
];

const initialEdges = [
  { id: "edge-api-callback-exec", source: "api-1", sourceHandle: "next", target: "callback-1", targetHandle: "trigger" },
  {
    id: "edge-api-callback-key",
    source: "api-1",
    sourceHandle: "primaryKey",
    target: "callback-1",
    targetHandle: "correlationKey",
    data: { mapping: { from: "$.api.outputs.primaryKey", to: "$.callback.inputs.correlationKey" } },
  },
  { id: "edge-api-query-exec", source: "api-1", sourceHandle: "next", target: "query-1", targetHandle: "trigger" },
  {
    id: "edge-api-query-key",
    source: "api-1",
    sourceHandle: "primaryKey",
    target: "query-1",
    targetHandle: "queryKey",
    data: { mapping: { from: "$.api.outputs.primaryKey", to: "$.query.inputs.queryKey" } },
  },
  {
    id: "edge-query-assert-data",
    source: "query-1",
    sourceHandle: "response",
    target: "assert-1",
    targetHandle: "evidence",
    data: { mapping: { from: "$.query.outputs.response", to: "$.assert.inputs.evidence" } },
  },
];

const interfaceFilterKeys = ["domain", "scenario", "serviceId", "actionType"];

function unique(values) {
  return Array.from(new Set(values.filter(Boolean)));
}

export function filterAtomicInterfaces(filters = {}) {
  return atomicInterfaceCatalog.filter((item) =>
    interfaceFilterKeys.every((key) => !filters[key] || item[key] === filters[key]),
  );
}

export function interfaceFilterOptions(filters = {}) {
  const domainScoped = filterAtomicInterfaces({ domain: filters.domain });
  const scenarioScoped = filterAtomicInterfaces({
    domain: filters.domain,
    scenario: filters.scenario,
  });
  const serviceScoped = filterAtomicInterfaces({
    domain: filters.domain,
    scenario: filters.scenario,
    serviceId: filters.serviceId,
  });
  return {
    domains: unique(atomicInterfaceCatalog.map((item) => item.domain)),
    scenarios: unique(domainScoped.map((item) => item.scenario)),
    services: unique(scenarioScoped.map((item) => item.serviceId)),
    actions: unique(serviceScoped.map((item) => item.actionType)),
  };
}

function stepPrimaryKeyBinding(step) {
  const hint = (step.databaseHints || [])[0];
  if (hint?.table || hint?.key) {
    return [hint.table, hint.key].filter(Boolean).join(" · ");
  }
  return "待 AI 根据请求模板、响应字段和证据确认";
}

function templateFromWorkflowStep(step) {
  return {
    type: "API_CASE_STEP",
    name: step.displayName || step.id,
    service: step.serviceId || "unassigned",
    inputs: [
      { id: "trigger", name: "触发", direction: "IN", portType: "EXEC", required: true },
      { id: "inputKey", name: "关联主键", direction: "IN", portType: "DATA", dataType: "key" },
    ],
    outputs: [
      { id: "next", name: "下一接口", direction: "OUT", portType: "EXEC" },
      { id: "response", name: "接口响应", direction: "OUT", portType: "DATA", dataType: "object" },
      { id: "primaryKey", name: "输出主键", direction: "OUT", portType: "DATA", dataType: "key" },
    ],
    defaultConfig: {
      stepId: step.id,
      displayName: step.displayName || step.id,
      caseId: step.caseId || "",
      action: step.action || "",
      serviceId: step.serviceId || "",
      requestTemplate: step.caseId ? `api-case://${step.caseId}` : "待关联 API Case",
      primaryKeyBinding: stepPrimaryKeyBinding(step),
      aiConfirmation: "确认请求参数模板、接口输出字段、关联主键和断言证据",
      evidenceKinds: (step.evidenceKinds || []).join(", "),
    },
  };
}

export function templateByType(type) {
  return nodeTemplates.find((template) => template.type === type);
}

export function makeBlueprintNode(template, index, position, id = `${template.type.toLowerCase()}-${index}`) {
  return {
    id,
    type: "blueprintNode",
    position,
    data: {
      template,
      config: { ...(template.defaultConfig || {}) },
    },
  };
}

function schemaPortId(prefix, field) {
  return `${prefix}__${field.id.replace(/[^a-zA-Z0-9_]+/g, "_")}`;
}

function schemaFieldToPort(prefix, direction, field) {
  return {
    id: schemaPortId(prefix, field),
    name: field.name || field.id,
    direction,
    portType: "DATA",
    dataType: field.dataType || "any",
    required: Boolean(field.required),
    path: field.path || `$.${prefix}.${field.id}`,
    fieldId: field.id,
    fieldRole: prefix === "param" ? "REQUEST_PARAM" : "RESPONSE_FIELD",
  };
}

function templateFromAtomicInterface(interfaceItem) {
  const baseTemplate = templateByType(interfaceItem.templateType) || templateByType("API_STEP");
  return {
    ...baseTemplate,
    inputs: [
      ...baseTemplate.inputs,
      ...(interfaceItem.requestParams || []).map((field) => schemaFieldToPort("param", "IN", field)),
    ],
    outputs: [
      ...baseTemplate.outputs,
      ...(interfaceItem.responseFields || []).map((field) => schemaFieldToPort("response", "OUT", field)),
    ],
  };
}

function configFromInterface(interfaceItem, template) {
  return {
    ...(template.defaultConfig || {}),
    catalogInterfaceId: interfaceItem.id,
    interfaceBindingStatus: "BOUND",
    domain: interfaceItem.domain,
    scenario: interfaceItem.scenario,
    actionType: interfaceItem.actionType,
    displayName: interfaceItem.name,
    serviceId: interfaceItem.serviceId,
    caseId: interfaceItem.caseId || "",
    operation: interfaceItem.operation || "",
    requestTemplate: interfaceItem.caseId ? `api-case://${interfaceItem.caseId}` : "待关联 API Case",
    primaryKeyBinding: interfaceItem.primaryKeyBinding || "待确认",
    aiConfirmation: interfaceItem.aiConfirmation || "确认请求参数模板、接口输出字段、关联主键和断言证据",
    requestParams: (interfaceItem.requestParams || []).map((field) => ({ ...field })),
    responseFields: (interfaceItem.responseFields || []).map((field) => ({ ...field })),
  };
}

export function makePlaceholderInterfaceNode(index, position) {
  const template = templateByType("API_STEP");
  const node = makeBlueprintNode(template, index, position, `interface-step-${index}`);
  return {
    ...node,
    data: {
      ...node.data,
      config: {
        ...(template.defaultConfig || {}),
        displayName: `接口步骤 ${index}`,
        serviceId: "待绑定",
        caseId: "",
        operation: "",
        requestTemplate: "待绑定 API Case",
        primaryKeyBinding: "待绑定",
        aiConfirmation: "先画流程，再选择原子接口并由 AI 确认参数模板和主键映射",
        interfaceBindingStatus: "UNBOUND",
      },
    },
  };
}

export function applyInterfaceToBlueprintNode(node, interfaceItem) {
  const template = templateFromAtomicInterface(interfaceItem);
  return {
    ...node,
    data: {
      ...node.data,
      template,
      config: configFromInterface(interfaceItem, template),
    },
  };
}

export function makeBlueprintNodeFromInterface(interfaceItem, index, position) {
  const template = templateFromAtomicInterface(interfaceItem);
  const safeId = interfaceItem.id.replace(/[^a-zA-Z0-9]+/g, "-");
  const node = makeBlueprintNode(template, index, position, `${safeId}-${index}`);
  return {
    ...node,
    data: {
      ...node.data,
      config: configFromInterface(interfaceItem, template),
    },
  };
}

export function buildInitialBlueprint() {
  return {
    nodes: initialNodeSpecs.map((spec, index) => {
      const template = templateByType(spec.templateType);
      return makeBlueprintNode(template, index + 1, spec.position, spec.id);
    }),
    edges: initialEdges.map((edge) => ({ ...edge })),
  };
}

export function buildBlueprintFromWorkflow(workflow) {
  const steps = workflow?.steps || [];
  const nodes = steps.map((step, index) => ({
    ...makeBlueprintNode(templateFromWorkflowStep(step), index + 1, {
      x: 60 + (index % 5) * 300,
      y: 90 + Math.floor(index / 5) * 250,
    }, step.id),
    data: {
      template: templateFromWorkflowStep(step),
      config: { ...templateFromWorkflowStep(step).defaultConfig },
      workflowId: workflow.id,
      workflowName: workflow.displayName || workflow.id,
    },
  }));
  const edges = [];
  for (let index = 1; index < nodes.length; index += 1) {
    const previous = nodes[index - 1];
    const current = nodes[index];
    edges.push({
      id: `edge-${previous.id}-${current.id}-exec`,
      source: previous.id,
      sourceHandle: "next",
      target: current.id,
      targetHandle: "trigger",
    });
    edges.push({
      id: `edge-${previous.id}-${current.id}-key`,
      source: previous.id,
      sourceHandle: "primaryKey",
      target: current.id,
      targetHandle: "inputKey",
      data: {
        mapping: {
          from: `$.${previous.id}.outputs.primaryKey`,
          to: `$.${current.id}.inputs.inputKey`,
        },
      },
    });
  }
  return { nodes, edges };
}

export function portFor(node, portId, direction) {
  const ports = direction === "OUT" ? node?.data?.template?.outputs : node?.data?.template?.inputs;
  return (ports || []).find((port) => port.id === portId);
}

export function portPairFor(nodes, edge) {
  const nodeById = new Map(nodes.map((node) => [node.id, node]));
  const source = nodeById.get(edge.source);
  const target = nodeById.get(edge.target);
  return {
    source,
    target,
    sourcePort: portFor(source, edge.sourceHandle, "OUT"),
    targetPort: portFor(target, edge.targetHandle, "IN"),
  };
}

export function edgeKind(nodes, edge) {
  const { sourcePort, targetPort } = portPairFor(nodes, edge);
  if (!sourcePort || !targetPort) return "UNKNOWN";
  if (sourcePort.portType !== targetPort.portType) return "INVALID";
  return sourcePort.portType;
}

export function validateConnection(nodes, connection) {
  const kind = edgeKind(nodes, connection);
  if (kind === "UNKNOWN") return "Connection references a missing node or port";
  if (kind === "INVALID") {
    const { sourcePort, targetPort } = portPairFor(nodes, connection);
    return `${connection.id || "connection"}: ${sourcePort.portType} output cannot connect to ${targetPort.portType} input`;
  }
  return "";
}

export function validateBlueprint(nodes, edges) {
  return edges.map((edge) => validateConnection(nodes, edge)).filter(Boolean);
}

function mappingForEdge(nodes, edge) {
  if (edge.data?.mapping && Object.keys(edge.data.mapping).length) {
    return edge.data.mapping;
  }
  const { sourcePort, targetPort } = portPairFor(nodes, edge);
  if (sourcePort?.portType === "DATA" && targetPort?.portType === "DATA") {
    return {
      from: sourcePort.path || `$.${edge.source}.outputs.${edge.sourceHandle}`,
      to: targetPort.path || `$.${edge.target}.inputs.${edge.targetHandle}`,
      sourceField: sourcePort.fieldId || edge.sourceHandle,
      targetField: targetPort.fieldId || edge.targetHandle,
    };
  }
  return {};
}

export function exportBlueprintConfig(nodes, edges, workflowMeta = {}) {
  return {
    schemaVersion: "workflow-blueprint-demo/v1",
    workflowId: workflowMeta.id || nodes[0]?.data?.workflowId || "draft.workflow",
    workflowName: workflowMeta.name || nodes[0]?.data?.workflowName || "新建工作流",
    nodes: nodes.map((node) => ({
      id: node.id,
      type: node.data.template.type,
      name: node.data.config?.displayName || node.data.template.name,
      service: node.data.config?.serviceId || node.data.template.service,
      position: node.position,
      config: node.data.config || {},
      ports: {
        inputs: node.data.template.inputs,
        outputs: node.data.template.outputs,
      },
    })),
    edges: edges.map((edge) => ({
      id: edge.id,
      kind: edgeKind(nodes, edge),
      from: edge.source,
      outPort: edge.sourceHandle,
      to: edge.target,
      inPort: edge.targetHandle,
      mapping: mappingForEdge(nodes, edge),
    })),
  };
}
