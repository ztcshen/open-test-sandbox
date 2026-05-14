import { useEffect, useMemo, useState } from "react";
import { createRoot } from "react-dom/client";
import { RefreshCw } from "lucide-react";
import { fetchJSON } from "./environmentCommon.jsx";

const roleLabels = {
  app: "App",
  platform: "Platform",
  support: "Support",
  middleware: "Middleware",
  external: "External",
};

function runtimeMap(snapshot = {}) {
  snapshot = snapshot || {};
  const byId = new Map();
  (snapshot.groups || []).forEach((group) => (group.items || []).forEach((item) => byId.set(item.id, item)));
  return byId;
}

function statusText(runtime) {
  if (!runtime) return "未纳入运行快照";
  if (runtime.state === "missing") return "离线";
  if (runtime.health && runtime.health !== "unknown") return runtime.health;
  return runtime.state || "unknown";
}

function workflowUsage(workflows = []) {
  const usage = new Map();
  workflows.forEach((workflow) => {
    const serviceIds = [...new Set((workflow.steps || []).map((step) => step.serviceId).filter(Boolean))];
    serviceIds.forEach((serviceId) => usage.set(serviceId, (usage.get(serviceId) || 0) + 1));
  });
  return usage;
}

function serviceHref(service) {
  if (service.role === "external") return `#service-${encodeURIComponent(service.id || "external")}`;
  return `/environment-node.html?id=${encodeURIComponent(service.id || "")}`;
}

function SourceStatus({ catalog }) {
  const warnings = catalog?.warnings || [];
  const ok = catalog?.source?.ok !== false;
  const source = catalog?.source?.kind === "manifest" ? "Manifest" : "Catalog";
  const version = catalog?.schemaVersion ? ` v${catalog.schemaVersion}` : "";
  const warningText = warnings.length ? ` · ${warnings.length} warnings` : "";
  return (
    <div className={`catalog-source-status ${ok && !warnings.length ? "ok" : "warning"}`} title={[catalog?.source?.path || catalog?.source?.error || "", warnings.join("\n")].filter(Boolean).join("\n")}>
      {ok ? `${source}${version}${warningText}` : `${source}${version}: fallback${warningText}`}
    </div>
  );
}

function InventoryStats({ services }) {
  const counts = services.reduce((acc, service) => {
    const role = service.role || "unknown";
    acc[role] = (acc[role] || 0) + 1;
    return acc;
  }, {});
  return (
    <div className="service-inventory-stats" aria-label="服务清单摘要">
      {["app", "support", "middleware", "platform", "external"].map((role) => (
        <div key={role}>
          <span>{roleLabels[role] || role}</span>
          <strong>{counts[role] || 0}</strong>
        </div>
      ))}
    </div>
  );
}

function ServiceCard({ service, runtime, usageCount }) {
  const rows = [
    ["id", service.id || "-"],
    ["port", service.port ? `:${service.port}` : "-"],
    ["runtime", statusText(runtime)],
    ["container", runtime?.container || "-"],
    ["health", runtime?.health || "-"],
    ["repo", service.repoEnv || "-"],
    ["mock", service.mockable ? "yes" : "no"],
    ["downstream", service.dependencies?.length ? service.dependencies.join(", ") : "-"],
    ["workflows", String(usageCount || 0)],
  ];
  return (
    <article className={`service-inventory-card ${runtime?.ok ? "ok" : runtime?.state === "missing" ? "missing" : "unknown"}`} id={`service-${service.id || "unknown"}`}>
      <div className="service-inventory-card-top">
        <a className="service-inventory-service-link" href={serviceHref(service)}>{service.displayName || service.id}</a>
        <span>{statusText(runtime)}</span>
      </div>
      <dl className="service-inventory-meta">
        {rows.map(([key, value]) => (
          <div key={key}>
            <dt>{key}</dt>
            <dd>{value}</dd>
          </div>
        ))}
      </dl>
    </article>
  );
}

function ServiceGroup({ role, services, runtimeById, usage }) {
  return (
    <section className="service-inventory-group">
      <div className="service-inventory-group-head">
        <h3>{roleLabels[role] || role}</h3>
        <span>{`${services.length} services`}</span>
      </div>
      <div className="service-inventory-card-grid">
        {services.length ? (
          services.map((service) => (
            <ServiceCard service={service} runtime={runtimeById.get(service.id)} usageCount={usage.get(service.id)} key={service.id} />
          ))
        ) : (
          <p className="dashboard-empty">No catalog service in this role.</p>
        )}
      </div>
    </section>
  );
}

function Topology({ catalog }) {
  const edges = catalog?.topology?.edges || [];
  return (
    <div className="service-inventory-topology">
      {!edges.length ? <p className="dashboard-empty">No topology edges declared.</p> : null}
      {edges.map((edge) => (
        <div className="service-inventory-edge" key={`${edge.from}-${edge.to}`}>
          <strong>{edge.from || "-"}</strong>
          <span>{"->"}</span>
          <strong>{edge.to || "-"}</strong>
        </div>
      ))}
    </div>
  );
}

function ServiceInventoryApp() {
  const [catalog, setCatalog] = useState(null);
  const [dashboard, setDashboard] = useState(null);
  const [message, setMessage] = useState("loading");

  async function refresh() {
    setMessage("refreshing...");
    try {
      const [nextCatalog, nextDashboard] = await Promise.all([fetchJSON("/api/catalog"), fetchJSON("/api/dashboard")]);
      setCatalog(nextCatalog);
      setDashboard(nextDashboard);
      setMessage("ready");
    } catch (error) {
      setMessage(`failed: ${error.message}`);
    }
  }

  useEffect(() => {
    refresh();
  }, []);

  const services = catalog?.services || [];
  const summary = dashboard?.summary || {};
  const runtimeById = useMemo(() => runtimeMap(dashboard), [dashboard]);
  const usage = useMemo(() => workflowUsage(catalog?.workflows || []), [catalog]);
  return (
    <main className="app service-inventory-page service-inventory-shell" data-template-id="TPL-SERVICE-INVENTORY-V1">
      <div className="template-watermark" aria-label="模板编号">TPL-SERVICE-INVENTORY-V1</div>
      <section className="topbar">
        <div>
          <h1>服务清单</h1>
          <p>{`${services.length} services · ${summary.healthy || 0}/${summary.total || 0} online`}</p>
        </div>
        <InventoryStats services={services} />
        <div className="actions">
          <span className="dashboard-status-pill" role="status">{message}</span>
          <a className="button-link" href="/">控制台</a>
          <a className="button-link" href="/dashboard.html">环境大盘</a>
          <a className="button-link" href="/environment-nodes.html">环境节点</a>
          <button type="button" title="刷新" onClick={refresh}>
            <RefreshCw size={15} aria-hidden="true" />
            <span>刷新</span>
          </button>
        </div>
      </section>
      <section className="service-inventory-layout" aria-label="Catalog 服务清单">
        <section className="service-inventory-main">
          <div className="dashboard-section-head">
            <div>
              <h2>Catalog services</h2>
              <p>Manifest 中声明的 app / platform / support / external 服务。</p>
            </div>
            <SourceStatus catalog={catalog || {}} />
          </div>
          <div className="service-inventory-groups">
            {["app", "platform", "support", "external"].map((role) => (
              <ServiceGroup role={role} services={services.filter((service) => service.role === role)} runtimeById={runtimeById} usage={usage} key={role} />
            ))}
          </div>
        </section>
        <aside className="service-inventory-side">
          <div className="dashboard-section-head">
            <div>
              <h2>Topology edges</h2>
              <p>{`${catalog?.topology?.edges?.length || 0} edges · ${catalog?.topology?.nodes?.length || 0} nodes`}</p>
            </div>
          </div>
          <Topology catalog={catalog} />
        </aside>
      </section>
    </main>
  );
}

createRoot(document.getElementById("react-service-inventory-root")).render(<ServiceInventoryApp />);
