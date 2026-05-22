const safeDatabaseNamePattern = /(^|[_-])agent[_-]testbench([_-]|$)|(^|[_-])(smoke|test|ci)([_-]|$)/i;

export function inspectMySQLStoreDSN(rawValue) {
  const raw = String(rawValue || "").trim();
  try {
    const parsed = new URL(raw);
    const database = decodeURIComponent(parsed.pathname.replace(/^\/+/, ""));
    const masked = new URL(raw);
    if (masked.password) masked.password = "xxxxx";
    return {
      parseOK: true,
      scheme: parsed.protocol.replace(/:$/, "").toLowerCase(),
      database,
      safeName: safeDatabaseNamePattern.test(database),
      masked: masked.toString(),
    };
  } catch (error) {
    return {
      parseOK: false,
      scheme: "",
      database: "",
      safeName: false,
      masked: "",
      error: error.message,
    };
  }
}

export function requireSafeMySQLStoreDSN(rawValue, { label = "MySQL Store" } = {}) {
  const info = inspectMySQLStoreDSN(rawValue);
  if (!info.parseOK || info.scheme !== "mysql" || !info.database) {
    throw new Error(`${label} requires a mysql:// Store DSN with a database path`);
  }
  if (!info.safeName) {
    throw new Error(`${label} refuses database '${info.database}'; use a dedicated sandbox/smoke/test/ci database name`);
  }
  return info;
}

if (process.argv[1] && import.meta.url === new URL(process.argv[1], "file:").href) {
  process.stdout.write(JSON.stringify(inspectMySQLStoreDSN(process.argv[2] || "")));
}
