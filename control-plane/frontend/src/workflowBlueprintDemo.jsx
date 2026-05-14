import React, { useCallback, useEffect, useMemo, useState } from "react";
import { createRoot } from "react-dom/client";
import {
  addEdge,
  Background,
  Controls,
  Handle,
  MarkerType,
  MiniMap,
  Position,
  ReactFlow,
  ReactFlowProvider,
  useEdgesState,
  useNodesState,
  useReactFlow,
} from "@xyflow/react";
import "@xyflow/react/dist/style.css";
import "./workflowBlueprintDemo.css";
import {
  applyInterfaceToBlueprintNode,
  filterAtomicInterfaces,
  buildBlueprintFromWorkflow,
  edgeKind,
  exportBlueprintConfig,
  interfaceFilterOptions,
  makeBlueprintNode,
  makeBlueprintNodeFromInterface,
  makePlaceholderInterfaceNode,
  nodeTemplates,
  portPairFor,
  validateBlueprint,
  validateConnection,
} from "./workflowBlueprintModel.mjs";
import { fetchJSON } from "./api.js";

const defaultWorkflowId = "workflow.alpha";
const blankWorkflowMeta = { id: "draft.workflow", name: "新建工作流" };
const interfaceFilterOrder = ["domain", "scenario", "serviceId", "actionType"];
const nodePortLayout = {
  headHeight: 74,
  listTop: 10,
  rowHeight: 32,
};

function portTop(index) {
  return `${nodePortLayout.headHeight + nodePortLayout.listTop + index * nodePortLayout.rowHeight + nodePortLayout.rowHeight / 2}px`;
}

function portTone(port) {
  return port.portType === "EXEC" ? "exec" : "data";
}

function BlueprintNode({ data, selected }) {
  const { template } = data;
  const displayName = data.config?.displayName || template.name;
  const serviceName = data.config?.serviceId || template.service;
  const portCount = Math.max(template.inputs.length, template.outputs.length, 1);
  const layoutStyle = {
    "--node-head-height": `${nodePortLayout.headHeight}px`,
    "--port-list-top": `${nodePortLayout.listTop}px`,
    "--port-row-height": `${nodePortLayout.rowHeight}px`,
    "--port-count": portCount,
  };
  return (
    <div className={`blueprint-node ${selected ? "selected" : ""}`} style={layoutStyle}>
      {template.inputs.map((port, index) => (
        <Handle
          aria-label={`输入 ${port.name}`}
          className={`blueprint-handle ${portTone(port)}`}
          data-port-id={port.id}
          id={port.id}
          key={port.id}
          position={Position.Left}
          style={{ top: portTop(index) }}
          type="target"
        />
      ))}
      {template.outputs.map((port, index) => (
        <Handle
          aria-label={`输出 ${port.name}`}
          className={`blueprint-handle ${portTone(port)}`}
          data-port-id={port.id}
          id={port.id}
          key={port.id}
          position={Position.Right}
          style={{ top: portTop(index) }}
          type="source"
        />
      ))}
      <div className="blueprint-node-head">
        <span>{template.type}</span>
        <strong>{displayName}</strong>
        <em>{serviceName}</em>
      </div>
      <div className="blueprint-node-ports">
        <div>
          {template.inputs.map((port) => (
            <div className={`blueprint-port ${portTone(port)}`} key={port.id}>
              <span>{port.name}</span>
              <em>{port.portType}</em>
            </div>
          ))}
        </div>
        <div>
          {template.outputs.map((port) => (
            <div className={`blueprint-port ${portTone(port)} out`} key={port.id}>
              <span>{port.name}</span>
              <em>{port.portType}</em>
            </div>
          ))}
        </div>
      </div>
    </div>
  );
}

const nodeTypes = { blueprintNode: BlueprintNode };

function configEntries(config) {
  return Object.entries(config || {});
}

function parseConfigValue(current, nextValue) {
  if (typeof current === "number") {
    const parsed = Number(nextValue);
    return Number.isFinite(parsed) ? parsed : current;
  }
  if (typeof current === "boolean") return nextValue === "true";
  return nextValue;
}

function BlueprintEditor() {
  const rootEl = document.getElementById("react-workflow-blueprint-demo-root");
  const blueprintMode = rootEl?.dataset?.blueprintMode || "demo";
  const isNewOnly = blueprintMode === "new";
  const templateId = rootEl?.dataset?.templateId || "TPL-WORKFLOW-BLUEPRINT-DEMO-V1";
  const [nodes, setNodes, onNodesChange] = useNodesState([]);
  const [edges, setEdges, onEdgesChange] = useEdgesState([]);
  const [selected, setSelected] = useState({ kind: "none", id: "" });
  const [message, setMessage] = useState("blank draft");
  const [workflowMeta, setWorkflowMeta] = useState(blankWorkflowMeta);
  const [interfaceFilters, setInterfaceFilters] = useState({
    domain: "",
    scenario: "",
    serviceId: "",
    actionType: "",
  });
  const [contextMenu, setContextMenu] = useState(null);
  const [detailNodeId, setDetailNodeId] = useState("");
  const [nextIndex, setNextIndex] = useState(5);
  const { screenToFlowPosition } = useReactFlow();

  const selectedNode = selected.kind === "node" ? nodes.find((node) => node.id === selected.id) : null;
  const selectedEdge = selected.kind === "edge" ? edges.find((edge) => edge.id === selected.id) : null;
  const detailNode = detailNodeId ? nodes.find((node) => node.id === detailNodeId) : null;
  const validation = useMemo(() => validateBlueprint(nodes, edges), [nodes, edges]);
  const exported = useMemo(() => exportBlueprintConfig(nodes, edges, workflowMeta), [edges, nodes, workflowMeta]);
  const filtersReady = interfaceFilterOrder.every((key) => interfaceFilters[key]);
  const filterOptions = useMemo(() => interfaceFilterOptions(interfaceFilters), [interfaceFilters]);
  const candidateInterfaces = useMemo(
    () => (filtersReady ? filterAtomicInterfaces(interfaceFilters) : []),
    [filtersReady, interfaceFilters],
  );

  const loadConfiguredWorkflow = useCallback(async () => {
    if (isNewOnly) {
      setMessage("blank draft");
      return;
    }
    const catalog = await fetchJSON("/api/catalog");
    const requested = new URLSearchParams(window.location.search).get("workflow");
    if (!requested) {
      setMessage("blank draft");
      return;
    }
    const workflows = catalog.workflows || [];
    const workflow =
      workflows.find((item) => item.id === requested) ||
      workflows.find((item) => item.id === defaultWorkflowId);
    if (!workflow) return;
    const nextBlueprint = buildBlueprintFromWorkflow(workflow);
    setNodes(nextBlueprint.nodes);
    setEdges(nextBlueprint.edges.map((edge) => decorateEdge(nextBlueprint.nodes, edge)));
    setWorkflowMeta({ id: workflow.id, name: workflow.displayName || workflow.id });
    setNextIndex((workflow.steps || []).length + 1);
    const preferredNode = nextBlueprint.nodes.find((node) => node.id === "apply") || nextBlueprint.nodes[0];
    setSelected(preferredNode ? { kind: "node", id: preferredNode.id } : { kind: "none", id: "" });
    setMessage(`catalog workflow · ${(workflow.steps || []).length} steps`);
  }, [setEdges, setNodes]);

  useEffect(() => {
    let cancelled = false;
    async function loadCatalogWorkflow() {
      try {
        if (!cancelled) await loadConfiguredWorkflow();
      } catch (error) {
        if (!cancelled) setMessage(`catalog fallback · ${error.message}`);
      }
    }
    loadCatalogWorkflow();
    return () => {
      cancelled = true;
    };
  }, [isNewOnly, loadConfiguredWorkflow]);

  const addTemplateAt = useCallback(
    (template, position) => {
      const node = makeBlueprintNode(template, nextIndex, position);
      setNodes((items) => items.concat(node));
      setSelected({ kind: "node", id: node.id });
      setNextIndex((value) => value + 1);
    },
    [nextIndex, setNodes],
  );

  const addInterfaceAt = useCallback(
    (interfaceItem, position) => {
      const node = makeBlueprintNodeFromInterface(interfaceItem, nextIndex, position);
      setNodes((items) => items.concat(node));
      setSelected({ kind: "node", id: node.id });
      setNextIndex((value) => value + 1);
      setMessage("ready");
    },
    [nextIndex, setNodes],
  );

  const addPlaceholderAt = useCallback(
    (position) => {
      const node = makePlaceholderInterfaceNode(nextIndex, position);
      setNodes((items) => items.concat(node));
      setSelected({ kind: "node", id: node.id });
      setNextIndex((value) => value + 1);
      setContextMenu(null);
      setMessage("placeholder ready");
    },
    [nextIndex, setNodes],
  );

  const updateInterfaceFilter = useCallback((key, value) => {
    setInterfaceFilters((current) => {
      const next = { ...current, [key]: value };
      const changedIndex = interfaceFilterOrder.indexOf(key);
      interfaceFilterOrder.slice(changedIndex + 1).forEach((laterKey) => {
        next[laterKey] = "";
      });
      return next;
    });
  }, []);

  const onConnect = useCallback(
    (connection) => {
      const error = validateConnection(nodes, connection);
      if (error) {
        setMessage(error);
        return;
      }
      const { sourcePort, targetPort } = portPairFor(nodes, connection);
      const mapping =
        sourcePort?.portType === "DATA" && targetPort?.portType === "DATA"
          ? {
              from: sourcePort.path || `$.${connection.source}.outputs.${connection.sourceHandle}`,
              to: targetPort.path || `$.${connection.target}.inputs.${connection.targetHandle}`,
              sourceField: sourcePort.fieldId || connection.sourceHandle,
              targetField: targetPort.fieldId || connection.targetHandle,
            }
          : {};
      const id = `edge-${connection.source}-${connection.sourceHandle}-${connection.target}-${connection.targetHandle}-${Date.now()}`;
      const nextEdge = decorateEdge(nodes, {
        ...connection,
        id,
        data: { mapping },
      });
      setEdges((items) => addEdge(nextEdge, items));
      setSelected({ kind: "edge", id });
      setMessage("ready");
    },
    [nodes, setEdges],
  );

  const onDrop = useCallback(
    (event) => {
      event.preventDefault();
      const isPlaceholder = event.dataTransfer.getData("application/workflow-placeholder-node");
      if (isPlaceholder) {
        addPlaceholderAt(screenToFlowPosition({ x: event.clientX, y: event.clientY }));
        return;
      }
      const interfaceId = event.dataTransfer.getData("application/workflow-atomic-interface");
      if (interfaceId) {
        const interfaceItem = candidateInterfaces.find((item) => item.id === interfaceId);
        if (interfaceItem) {
          addInterfaceAt(interfaceItem, screenToFlowPosition({ x: event.clientX, y: event.clientY }));
        }
        return;
      }
      const templateType = event.dataTransfer.getData("application/workflow-node-template");
      const template = nodeTemplates.find((item) => item.type === templateType);
      if (!template) return;
      addTemplateAt(template, screenToFlowPosition({ x: event.clientX, y: event.clientY }));
    },
    [addInterfaceAt, addPlaceholderAt, addTemplateAt, candidateInterfaces, screenToFlowPosition],
  );

  const updateSelectedNodeConfig = useCallback(
    (key, value) => {
      if (!selectedNode) return;
      setNodes((items) =>
        items.map((node) => {
          if (node.id !== selectedNode.id) return node;
          return {
            ...node,
            data: {
              ...node.data,
              config: {
                ...node.data.config,
                [key]: parseConfigValue(node.data.config?.[key], value),
              },
            },
          };
        }),
      );
    },
    [selectedNode, setNodes],
  );

  const updateSelectedEdgeMapping = useCallback(
    (key, value) => {
      if (!selectedEdge) return;
      setEdges((items) =>
        items.map((edge) => {
          if (edge.id !== selectedEdge.id) return edge;
          return {
            ...edge,
            data: {
              ...(edge.data || {}),
              mapping: {
                ...(edge.data?.mapping || {}),
                [key]: value,
              },
            },
          };
        }),
      );
    },
    [selectedEdge, setEdges],
  );

  const bindSelectedNodeToInterface = useCallback(
    (interfaceItem) => {
      if (!selectedNode) return;
      setNodes((items) =>
        items.map((node) => (node.id === selectedNode.id ? applyInterfaceToBlueprintNode(node, interfaceItem) : node)),
      );
      setMessage("interface bound");
    },
    [selectedNode, setNodes],
  );

  const createDraftWorkflow = useCallback(() => {
    setNodes([]);
    setEdges([]);
    setWorkflowMeta(blankWorkflowMeta);
    setSelected({ kind: "none", id: "" });
    setDetailNodeId("");
    setInterfaceFilters({ domain: "", scenario: "", serviceId: "", actionType: "" });
    setNextIndex(1);
    setMessage("draft workflow");
  }, [setEdges, setNodes]);

  const deleteSelected = useCallback(() => {
    if (selected.kind === "node") {
      setNodes((items) => items.filter((node) => node.id !== selected.id));
      setEdges((items) => items.filter((edge) => edge.source !== selected.id && edge.target !== selected.id));
    } else if (selected.kind === "edge") {
      setEdges((items) => items.filter((edge) => edge.id !== selected.id));
    }
    setSelected({ kind: "none", id: "" });
  }, [selected, setEdges, setNodes]);

  const openContextMenu = useCallback(
    (event) => {
      event.preventDefault();
      setContextMenu({
        x: event.clientX,
        y: event.clientY,
        position: screenToFlowPosition({ x: event.clientX, y: event.clientY }),
      });
    },
    [screenToFlowPosition],
  );

  return (
    <main className={`blueprint-demo-shell ${isNewOnly ? "blueprint-new-shell" : ""}`}>
      <div className="template-watermark" aria-label="模板编号">{templateId}</div>
      <header className="blueprint-demo-topbar">
        <div>
          <span>api workflow blueprint</span>
          <h1>{isNewOnly ? "新建工作流" : "接口工作流蓝图 Demo"}</h1>
          <p>{workflowMeta.name}</p>
        </div>
        <nav>
          <button onClick={createDraftWorkflow} type="button">新建工作流</button>
          {!isNewOnly ? (
            <button onClick={() => loadConfiguredWorkflow().catch((error) => setMessage(error.message))} type="button">载入已配置</button>
          ) : null}
          <button disabled={selected.kind === "none"} onClick={deleteSelected} type="button">删除选中</button>
          <a href="/">控制台</a>
          {!isNewOnly ? <a href="/workflows.html">Workflow 目录</a> : null}
          {!isNewOnly ? <a href="/workflow-detail.html">Workflow 定义</a> : null}
        </nav>
      </header>

      <section className="blueprint-workflow-fields" aria-label="工作流元数据">
        <label>
          <span>workflowId</span>
          <input
            onChange={(event) => setWorkflowMeta((value) => ({ ...value, id: event.target.value }))}
            value={workflowMeta.id}
          />
        </label>
        <label>
          <span>workflowName</span>
          <input
            onChange={(event) => setWorkflowMeta((value) => ({ ...value, name: event.target.value }))}
            value={workflowMeta.name}
          />
        </label>
      </section>

      <section className="blueprint-demo-grid">
        <aside className="blueprint-template-list blueprint-interface-library" aria-label="原子接口库">
          <InterfaceLibrary
            candidates={candidateInterfaces}
            filterOptions={filterOptions}
            filters={interfaceFilters}
            filtersReady={filtersReady}
            onAdd={(interfaceItem) => addInterfaceAt(interfaceItem, { x: 160 + nodes.length * 300, y: 140 + (nodes.length % 2) * 44 })}
            onAddPlaceholder={() => addPlaceholderAt({ x: 120, y: 90 })}
            onFilterChange={updateInterfaceFilter}
          />
        </aside>

        <section className="blueprint-canvas-shell" aria-label="工作流画布">
          {!nodes.length ? (
            <div className="blueprint-empty-canvas">
              <strong>空白画布</strong>
              {!isNewOnly ? <span>可载入已配置 Workflow，或从接口库添加步骤。</span> : null}
            </div>
          ) : null}
          <ReactFlow
            colorMode="dark"
            edges={edges}
            fitView
            maxZoom={1.35}
            minZoom={0.35}
            nodes={nodes}
            nodeTypes={nodeTypes}
            onConnect={onConnect}
            onDragOver={(event) => {
              event.preventDefault();
              event.dataTransfer.dropEffect = "move";
            }}
            onDrop={onDrop}
            onEdgeClick={(_, edge) => setSelected({ kind: "edge", id: edge.id })}
            onEdgesChange={onEdgesChange}
            onNodeClick={(_, node) => setSelected({ kind: "node", id: node.id })}
            onNodesChange={onNodesChange}
            onPaneClick={() => {
              setSelected({ kind: "none", id: "" });
              setContextMenu(null);
            }}
            onPaneContextMenu={openContextMenu}
          >
            <MiniMap pannable zoomable />
            <Controls />
            <Background color="#314158" gap={24} size={1.2} />
          </ReactFlow>
          {contextMenu ? (
            <div className="blueprint-context-menu" style={{ left: contextMenu.x, top: contextMenu.y }}>
              <button onClick={() => addPlaceholderAt(contextMenu.position)} type="button">新建接口节点</button>
            </div>
          ) : null}
        </section>

        <aside className={`blueprint-config-panel ${selectedNode || selectedEdge ? "is-open" : ""}`} aria-label="配置面板">
          <div className="blueprint-panel-head">
            <span>接口配置</span>
            <strong>{selected.kind}</strong>
          </div>
          {selectedNode ? (
            <NodeInspector
              node={selectedNode}
              onBindInterface={bindSelectedNodeToInterface}
              onChange={updateSelectedNodeConfig}
              onOpenDetail={() => setDetailNodeId(selectedNode.id)}
            />
          ) : selectedEdge ? (
            <EdgeInspector edge={selectedEdge} nodes={nodes} onChange={updateSelectedEdgeMapping} />
          ) : (
            <div className="blueprint-empty">No selection</div>
          )}
        </aside>
      </section>

      <section className={`blueprint-bottom ${nodes.length ? "has-nodes" : ""}`}>
        <div className="blueprint-validation">
          <div className="blueprint-panel-head">
            <span>连线校验</span>
            <strong>{validation.length ? "blocked" : message}</strong>
          </div>
          <ul>
            {(validation.length ? validation : ["valid"]).map((item) => (
              <li key={item}>{item}</li>
            ))}
          </ul>
        </div>
        <div className="blueprint-json-preview">
          <div className="blueprint-panel-head">
            <span>工作流 JSON</span>
            <strong>{exported.nodes.length} nodes · {exported.edges.length} edges</strong>
          </div>
          <pre>{JSON.stringify(exported, null, 2)}</pre>
        </div>
      </section>
      {detailNode ? <InterfaceDetailModal node={detailNode} onClose={() => setDetailNodeId("")} /> : null}
    </main>
  );
}

function InterfaceLibrary({ candidates, filterOptions, filters, filtersReady, onAdd, onAddPlaceholder, onFilterChange }) {
  const fields = [
    { key: "domain", label: "业务域", options: filterOptions.domains, placeholder: "选择业务域" },
    {
      key: "scenario",
      label: "场景",
      options: filters.domain ? filterOptions.scenarios : [],
      placeholder: "选择场景",
      disabled: !filters.domain,
    },
    {
      key: "serviceId",
      label: "服务",
      options: filters.scenario ? filterOptions.services : [],
      placeholder: "选择服务",
      disabled: !filters.scenario,
    },
    {
      key: "actionType",
      label: "动作",
      options: filters.serviceId ? filterOptions.actions : [],
      placeholder: "选择动作",
      disabled: !filters.serviceId,
    },
  ];
  return (
    <>
      <div className="blueprint-panel-head">
        <span>原子接口库</span>
        <strong>{filtersReady ? `${candidates.length} 个` : "筛选"}</strong>
      </div>
      <button
        className="blueprint-template-button blueprint-placeholder-button"
        draggable
        onClick={onAddPlaceholder}
        onDragStart={(event) => {
          event.dataTransfer.setData("application/workflow-placeholder-node", "true");
          event.dataTransfer.effectAllowed = "move";
        }}
        type="button"
      >
        <span>SKETCH</span>
        <strong>接口占位</strong>
        <em>先画 A {"->"} B，再绑定具体原子接口</em>
      </button>
      <section className="blueprint-interface-filters">
        {fields.map((field) => (
          <label key={field.key}>
            <span>{field.label}</span>
            <select
              aria-label={field.label}
              disabled={field.disabled}
              onChange={(event) => onFilterChange(field.key, event.target.value)}
              value={filters[field.key]}
            >
              <option value="">{field.placeholder}</option>
              {field.options.map((option) => (
                <option key={option} value={option}>{option}</option>
              ))}
            </select>
          </label>
        ))}
      </section>
      <div className="blueprint-interface-results">
        {!filtersReady ? (
          <div className="blueprint-empty compact">先完成筛选</div>
        ) : candidates.length ? (
          candidates.map((interfaceItem, index) => (
            <button
              className="blueprint-template-button"
              draggable
              key={interfaceItem.id}
              onClick={() => onAdd(interfaceItem, index)}
              onDragStart={(event) => {
                event.dataTransfer.setData("application/workflow-atomic-interface", interfaceItem.id);
                event.dataTransfer.effectAllowed = "move";
              }}
              type="button"
            >
              <span>{interfaceItem.actionType}</span>
              <strong>{interfaceItem.name}</strong>
              <em>{interfaceItem.serviceId} · {interfaceItem.caseId}</em>
            </button>
          ))
        ) : (
          <div className="blueprint-empty compact">没有匹配接口</div>
        )}
      </div>
    </>
  );
}

function NodeInterfaceBinder({ node, onBindInterface }) {
  const [filters, setFilters] = useState({
    domain: node.data.config?.domain || "",
    scenario: node.data.config?.scenario || "",
    serviceId: node.data.config?.serviceId === "待绑定" ? "" : node.data.config?.serviceId || "",
    actionType: node.data.config?.actionType || "",
  });
  const filterOptions = useMemo(() => interfaceFilterOptions(filters), [filters]);
  const filtersReady = interfaceFilterOrder.every((key) => filters[key]);
  const candidates = useMemo(() => (filtersReady ? filterAtomicInterfaces(filters) : []), [filters, filtersReady]);
  const updateFilter = useCallback((key, value) => {
    setFilters((current) => {
      const next = { ...current, [key]: value };
      const changedIndex = interfaceFilterOrder.indexOf(key);
      interfaceFilterOrder.slice(changedIndex + 1).forEach((laterKey) => {
        next[laterKey] = "";
      });
      return next;
    });
  }, []);
  const fields = [
    { key: "domain", label: "绑定业务域", options: filterOptions.domains, placeholder: "选择业务域" },
    {
      key: "scenario",
      label: "绑定场景",
      options: filters.domain ? filterOptions.scenarios : [],
      placeholder: "选择场景",
      disabled: !filters.domain,
    },
    {
      key: "serviceId",
      label: "绑定服务",
      options: filters.scenario ? filterOptions.services : [],
      placeholder: "选择服务",
      disabled: !filters.scenario,
    },
    {
      key: "actionType",
      label: "绑定动作",
      options: filters.serviceId ? filterOptions.actions : [],
      placeholder: "选择动作",
      disabled: !filters.serviceId,
    },
  ];
  return (
    <section className="blueprint-node-binder">
      <div className="blueprint-panel-head">
        <span>绑定原子接口</span>
        <strong>{node.data.config?.interfaceBindingStatus || "BOUND"}</strong>
      </div>
      <div className="blueprint-interface-filters">
        {fields.map((field) => (
          <label key={field.key}>
            <span>{field.label}</span>
            <select
              aria-label={field.label}
              disabled={field.disabled}
              onChange={(event) => updateFilter(field.key, event.target.value)}
              value={filters[field.key]}
            >
              <option value="">{field.placeholder}</option>
              {field.options.map((option) => (
                <option key={option} value={option}>{option}</option>
              ))}
            </select>
          </label>
        ))}
      </div>
      <div className="blueprint-interface-results">
        {!filtersReady ? (
          <div className="blueprint-empty compact">选择后绑定到当前节点</div>
        ) : candidates.length ? (
          candidates.map((interfaceItem) => (
            <button
              className="blueprint-bind-button"
              key={interfaceItem.id}
              onClick={() => onBindInterface(interfaceItem)}
              type="button"
            >
              绑定到当前节点：{interfaceItem.name}
            </button>
          ))
        ) : (
          <div className="blueprint-empty compact">没有匹配接口</div>
        )}
      </div>
    </section>
  );
}

function InterfaceDetailModal({ node, onClose }) {
  const config = node.data.config || {};
  const requestParams = config.requestParams || [];
  const responseFields = config.responseFields || [];
  return (
    <div className="blueprint-detail-backdrop">
      <section
        aria-label={`接口详情 ${config.displayName || node.data.template.name}`}
        className="blueprint-detail-modal"
        role="dialog"
      >
        <header>
          <div>
            <span className="blueprint-meta-label">接口详情</span>
            <h2>{config.displayName || node.data.template.name}</h2>
            <p>{config.serviceId || node.data.template.service} · {config.operation || node.data.template.type}</p>
          </div>
          <button onClick={onClose} type="button">关闭接口详情</button>
        </header>
        <div className="blueprint-detail-summary">
          <span>caseId: {config.caseId || "未绑定"}</span>
          <span>requestTemplate: {config.requestTemplate || "未绑定"}</span>
          <span>catalogInterfaceId: {config.catalogInterfaceId || "未绑定"}</span>
        </div>
        <div className="blueprint-detail-grid">
          <InterfaceFieldTable fields={requestParams} title="请求参数" />
          <InterfaceFieldTable fields={responseFields} title="返回字段" />
        </div>
      </section>
    </div>
  );
}

function InterfaceFieldTable({ fields, title }) {
  return (
    <section className="blueprint-field-table">
      <div className="blueprint-panel-head">
        <span>{title}</span>
        <strong>{fields.length}</strong>
      </div>
      {fields.length ? (
        <div className="blueprint-field-rows">
          {fields.map((field) => (
            <div className="blueprint-field-row" key={field.id}>
              <strong>{field.name || field.id}</strong>
              <span>{field.path}</span>
              <em>{field.dataType || "any"}{field.required ? " · required" : ""}</em>
            </div>
          ))}
        </div>
      ) : (
        <div className="blueprint-empty compact">未配置字段</div>
      )}
    </section>
  );
}

function decorateEdge(nodes, edge) {
  const kind = edgeKind(nodes, edge);
  const isData = kind === "DATA";
  return {
    type: "smoothstep",
    markerEnd: { type: MarkerType.ArrowClosed, color: isData ? "#38bdf8" : "#f59e0b" },
    label: kind,
    style: { stroke: isData ? "#38bdf8" : "#f59e0b", strokeWidth: 2.4 },
    labelStyle: { fill: "#dbeafe", fontWeight: 700 },
    labelBgStyle: { fill: "#111827", fillOpacity: 0.92 },
    ...edge,
  };
}

function NodeInspector({ node, onBindInterface, onChange, onOpenDetail }) {
  const template = node.data.template;
  return (
    <div className="blueprint-inspector-body">
      <section>
        <span className="blueprint-meta-label">API Case Step</span>
        <h2>{node.data.config?.displayName || template.name}</h2>
        <p>{node.id} · {node.data.config?.serviceId || template.service} · {template.type}</p>
      </section>
      <button className="blueprint-detail-open" onClick={onOpenDetail} type="button">接口详情</button>
      <NodeInterfaceBinder key={node.id} node={node} onBindInterface={onBindInterface} />
      <section className="blueprint-form">
        {configEntries(node.data.config).map(([key, value]) => (
          <label key={key}>
            <span>{key}</span>
            {typeof value === "boolean" ? (
              <select value={String(value)} onChange={(event) => onChange(key, event.target.value)}>
                <option value="true">true</option>
                <option value="false">false</option>
              </select>
            ) : key === "primaryKeyBinding" || key === "aiConfirmation" ? (
              <textarea onChange={(event) => onChange(key, event.target.value)} value={String(value)} />
            ) : (
              <input
                onChange={(event) => onChange(key, event.target.value)}
                type={typeof value === "number" ? "number" : "text"}
                value={String(value)}
              />
            )}
          </label>
        ))}
      </section>
      <PortMatrix inputs={template.inputs} outputs={template.outputs} />
    </div>
  );
}

function EdgeInspector({ edge, nodes, onChange }) {
  const kind = edgeKind(nodes, edge);
  const mapping = edge.data?.mapping || {};
  return (
    <div className="blueprint-inspector-body">
      <section>
        <span className="blueprint-meta-label">Step Dependency</span>
        <h2>{kind}</h2>
        <p>{edge.source}.{edge.sourceHandle} {"->"} {edge.target}.{edge.targetHandle}</p>
      </section>
      <section className="blueprint-form">
        {["from", "to"].map((key) => (
          <label key={key}>
            <span>{key}</span>
            <input onChange={(event) => onChange(key, event.target.value)} type="text" value={mapping[key] || ""} />
          </label>
        ))}
      </section>
    </div>
  );
}

function PortMatrix({ inputs, outputs }) {
  return (
    <section className="blueprint-port-matrix">
      <div>
        <span className="blueprint-meta-label">Inputs</span>
        {inputs.map((port) => <PortChip port={port} key={port.id} />)}
      </div>
      <div>
        <span className="blueprint-meta-label">Outputs</span>
        {outputs.map((port) => <PortChip port={port} key={port.id} />)}
      </div>
    </section>
  );
}

function PortChip({ port }) {
  return (
    <span className={`blueprint-port-chip ${portTone(port)}`}>
      {port.name}
      <em>{port.portType}{port.dataType ? `/${port.dataType}` : ""}</em>
    </span>
  );
}

function WorkflowBlueprintDemoApp() {
  return (
    <ReactFlowProvider>
      <BlueprintEditor />
    </ReactFlowProvider>
  );
}

createRoot(document.getElementById("react-workflow-blueprint-demo-root")).render(<WorkflowBlueprintDemoApp />);
