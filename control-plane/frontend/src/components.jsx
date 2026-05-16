import {
  Activity,
  ArrowUpRight,
  Boxes,
  CheckCircle2,
  Gauge,
  GitBranch,
  LayoutDashboard,
  RefreshCw,
  Search,
  Server,
  Workflow,
  Wrench,
  X,
} from "lucide-react";
import { classNames, compactText } from "./api.js";
import {
  workflowActionLabel,
  workflowEntrypointHref,
  workflowKind,
  workflowKindLabel,
  workflowServiceIds,
  workflowStepHref,
} from "./workflowModel.js";

export const Icons = {
  Activity,
  ArrowUpRight,
  Boxes,
  CheckCircle2,
  Gauge,
  GitBranch,
  LayoutDashboard,
  RefreshCw,
  Search,
  Server,
  Workflow,
  Wrench,
  X,
};

export function Shell({ children, className }) {
  return (
    <main className={classNames("react-control-plane", className)}>
      <div className="react-command-shell">{children}</div>
    </main>
  );
}

export function ButtonLink({ href, children, primary = false, icon: Icon }) {
  return (
    <a className={classNames("react-button", primary && "primary")} href={href}>
      {Icon ? <Icon size={15} aria-hidden="true" /> : null}
      <span>{children}</span>
    </a>
  );
}

export function IconButton({ children, onClick, icon: Icon, title }) {
  return (
    <button className="react-icon-button" type="button" title={title} onClick={onClick}>
      {Icon ? <Icon size={15} aria-hidden="true" /> : null}
      <span>{children}</span>
    </button>
  );
}

export function Hero({ kicker, title, summary, actions, stats }) {
  return (
    <section className="react-hero">
      <div>
        <span className="react-kicker">{kicker}</span>
        <h1>{title}</h1>
        <p>{summary}</p>
      </div>
      <div className="react-actions">{actions}</div>
      {stats?.length ? (
        <div className="react-stat-grid">
          {stats.map((item) => (
            <article className="react-stat" key={item.label}>
              <span>{item.label}</span>
              <strong>{item.value}</strong>
            </article>
          ))}
        </div>
      ) : null}
    </section>
  );
}

export function Panel({ title, summary, label, action, dark = false, className, children }) {
  return (
    <section className={classNames("react-panel", dark && "react-dark-panel", className)}>
      <div className="react-panel-head">
        <div>
          {label ? <span className="react-panel-label">{label}</span> : null}
          <h2>{title}</h2>
          {summary ? <p>{summary}</p> : null}
        </div>
        {action}
      </div>
      {children}
    </section>
  );
}

export function ServiceChips({ workflow, services }) {
  const serviceById = new Map((services || []).map((service) => [service.id, service]));
  return (
    <div className="react-service-chips">
      {workflowServiceIds(workflow).slice(0, 7).map((serviceId) => {
        const service = serviceById.get(serviceId);
        return (
          <a
            className={classNames("react-chip", !service && "warn")}
            href={service?.role === "external" ? `/service-inventory.html#service-${encodeURIComponent(service.id || serviceId)}` : `/environment-node.html?id=${encodeURIComponent(serviceId)}`}
            key={serviceId}
          >
            {service?.displayName || `${serviceId} · 未建模`}
          </a>
        );
      })}
    </div>
  );
}

function runtimeImpactTone(tone) {
  if (tone === "ok") return "good";
  if (tone === "bad") return "bad";
  return "warn";
}

export function WorkflowCard({ workflow, services, compact = false, runtimeImpact, onRuntimeImpactClick }) {
  const kind = workflowKind(workflow);
  const latestRun = workflow.latestRun || null;
  const runCount = Number(workflow.runCount || 0);
  const runStatus = latestRun?.status || (runCount ? "unknown" : "no runs");
  const runTone = ["passed", "success", "ok"].includes(String(runStatus).toLowerCase())
    ? "good"
    : ["failed", "error"].includes(String(runStatus).toLowerCase())
      ? "bad"
      : "warn";
  return (
    <article className="react-card">
      <div className="react-card-top">
        <div className="react-card-title">{compactText(workflow.displayName || workflow.id)}</div>
        <span className={classNames("react-pill", kind === "businessFlow" ? "good" : "warn")}>
          {workflowKindLabel(workflow)}
        </span>
      </div>
      <p>{compactText(workflow.description, "按业务阶段查看请求、返回、日志和证据。")}</p>
      <div className="react-service-chips">
        <span className={classNames("react-pill", runTone)}>{`${runCount} runs · ${runStatus}`}</span>
        {runtimeImpact ? (
          <button
            className={classNames("react-pill", "workflow-impact-button", runtimeImpactTone(runtimeImpact.tone))}
            type="button"
            onClick={() => onRuntimeImpactClick?.(runtimeImpact.text)}
          >
            {runtimeImpact.text}
          </button>
        ) : null}
        {latestRun?.id ? (
          <a className="react-chip" href={`/workflow-run.html?id=${encodeURIComponent(latestRun.id)}`}>
            {latestRun.id}
          </a>
        ) : null}
      </div>
      <ServiceChips workflow={workflow} services={services} />
      <div className="react-step-strip">
        {(workflow.steps || []).slice(0, compact ? 7 : 12).map((step) => (
          <a href={workflowStepHref(workflow.id, step.id)} key={step.id}>
            {step.displayName || step.id}
          </a>
        ))}
      </div>
      <div className="react-card-actions">
        <ButtonLink href={`/workflow-detail.html?id=${encodeURIComponent(workflow.id || "")}`}>查看定义</ButtonLink>
        <ButtonLink href={workflowEntrypointHref(workflow)} primary icon={ArrowUpRight}>
          {workflowActionLabel(workflow)}
        </ButtonLink>
      </div>
    </article>
  );
}
