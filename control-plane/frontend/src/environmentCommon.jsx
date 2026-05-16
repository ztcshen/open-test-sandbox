export async function fetchJSON(path) {
  const response = await fetch(path, { headers: { Accept: "application/json" } });
  const body = await response.json().catch(() => ({}));
  if (!response.ok || body.ok === false) {
    throw new Error(body.error || response.statusText);
  }
  return body;
}

export function statusText(item = {}) {
  if (item.state === "missing") return "未运行";
  if (item.health && item.health !== "unknown") return item.health;
  return item.state || "unknown";
}

export function statusTone(item = {}) {
  if (item.ok) return "ok";
  if (item.state === "missing") return "missing";
  return "bad";
}

export function flattenNodes(snapshot = {}) {
  return (snapshot.groups || []).flatMap((group) =>
    (group.items || []).map((item) => ({
      ...item,
      groupId: group.id,
      groupLabel: group.label,
    })),
  );
}

export function runtimeByService(snapshot = {}) {
  return new Map((snapshot.serviceRuntime || []).map((item) => [item.serviceId, item]));
}

export function envCopy(snapshot = {}, item = {}, key, fallback) {
  return item?.presentation?.copy?.[key] || snapshot?.presentation?.copy?.[key] || fallback;
}

export function StatBox({ label, value }) {
  return (
    <div>
      <span>{label}</span>
      <strong>{value}</strong>
    </div>
  );
}

export function DetailList({ rows }) {
  return (
    <dl className="environment-node-detail-list">
      {rows.map(([label, value]) => (
        <div className="environment-detail-row" key={label}>
          <dt>{label}</dt>
          <dd>{value || "-"}</dd>
        </div>
      ))}
    </dl>
  );
}
