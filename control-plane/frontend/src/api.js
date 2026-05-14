export async function fetchJSON(path) {
  const response = await fetch(path, {
    headers: { Accept: "application/json" },
  });
  const body = await response.json();
  if (!response.ok) {
    throw new Error(body.error || response.statusText);
  }
  return body;
}

export function classNames(...values) {
  return values.filter(Boolean).join(" ");
}

export function unique(values) {
  return [...new Set(values.filter(Boolean))];
}

export function compactText(value, fallback = "-") {
  const text = String(value || "").replace(/\s+/g, " ").trim();
  return text || fallback;
}
