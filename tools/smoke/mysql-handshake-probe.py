#!/usr/bin/env python3
import argparse
import json
import socket
import struct
import sys
from urllib.parse import urlparse


def read_exact(sock, size):
    data = b""
    while len(data) < size:
        chunk = sock.recv(size - len(data))
        if not chunk:
            raise EOFError(f"connection closed after {len(data)} of {size} bytes")
        data += chunk
    return data


def open_direct(host, port, timeout):
    return socket.create_connection((host, port), timeout=timeout)


def open_socks5(proxy_host, proxy_port, host, port, timeout):
    sock = socket.create_connection((proxy_host, proxy_port), timeout=timeout)
    sock.settimeout(timeout)
    sock.sendall(b"\x05\x01\x00")
    if read_exact(sock, 2) != b"\x05\x00":
        raise OSError("SOCKS5 proxy rejected no-auth negotiation")
    host_bytes = host.encode("idna")
    sock.sendall(b"\x05\x01\x00\x03" + bytes([len(host_bytes)]) + host_bytes + struct.pack("!H", port))
    header = read_exact(sock, 4)
    if header[1] != 0:
        raise OSError(f"SOCKS5 connect failed with code {header[1]}")
    atyp = header[3]
    if atyp == 1:
        read_exact(sock, 4)
    elif atyp == 3:
        read_exact(sock, read_exact(sock, 1)[0])
    elif atyp == 4:
        read_exact(sock, 16)
    else:
        raise OSError(f"SOCKS5 proxy returned address type {atyp}")
    read_exact(sock, 2)
    return sock


def read_mysql_handshake(sock):
    header = read_exact(sock, 4)
    payload_len = header[0] | (header[1] << 8) | (header[2] << 16)
    sequence_id = header[3]
    if payload_len <= 0:
        raise OSError("MySQL handshake payload is empty")
    payload = read_exact(sock, min(payload_len, 256))
    protocol = payload[0]
    server_version = payload[1:].split(b"\x00", 1)[0].decode("utf-8", "replace")
    return {
        "payloadBytes": payload_len,
        "sequenceId": sequence_id,
        "protocol": protocol,
        "serverVersion": server_version,
    }


def target_from_args(args):
    host = args.host
    port = args.port
    if args.url:
        parsed = urlparse(args.url)
        host = parsed.hostname or host
        port = parsed.port or port
    if not host:
        raise ValueError("--host or --url is required")
    return host, port


def main():
    parser = argparse.ArgumentParser(description="Probe whether a MySQL TCP endpoint returns an initial handshake packet.")
    parser.add_argument("--url", help="MySQL URL; only host and port are used")
    parser.add_argument("--host")
    parser.add_argument("--port", type=int, default=3306)
    parser.add_argument("--timeout", type=float, default=10)
    parser.add_argument("--socks-host")
    parser.add_argument("--socks-port", type=int, default=1080)
    parser.add_argument("--json", action="store_true")
    args = parser.parse_args()

    report = {"ok": False, "backend": "mysql"}
    try:
        host, port = target_from_args(args)
        report.update({"host": host, "port": port})
        if args.socks_host:
            report["proxy"] = {"type": "socks5", "host": args.socks_host, "port": args.socks_port}
            sock = open_socks5(args.socks_host, args.socks_port, host, port, args.timeout)
        else:
            sock = open_direct(host, port, args.timeout)
        with sock:
            sock.settimeout(args.timeout)
            report["handshake"] = read_mysql_handshake(sock)
            report["ok"] = report["handshake"]["protocol"] == 10
            if not report["ok"]:
                report["error"] = f"unexpected MySQL protocol byte {report['handshake']['protocol']}"
    except Exception as exc:
        report["error"] = str(exc)

    if args.json:
        print(json.dumps(report, ensure_ascii=False, indent=2, sort_keys=True))
    elif report["ok"]:
        hs = report["handshake"]
        print(f"MySQL handshake OK: {report['host']}:{report['port']} protocol={hs['protocol']} server={hs['serverVersion']}")
    else:
        print(f"MySQL handshake failed: {report.get('host', '')}:{report.get('port', '')} {report.get('error', '')}", file=sys.stderr)
    return 0 if report["ok"] else 2


if __name__ == "__main__":
    raise SystemExit(main())
