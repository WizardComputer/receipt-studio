"""Separate-process HTTP benchmark client; credentials arrive via stdin only."""
import base64
import http.client
import json
import sys
import time
import urllib.parse
import uuid

config = json.load(sys.stdin)
url = urllib.parse.urlsplit(config["url"])
assert url.scheme == "http" and url.hostname == "127.0.0.1", "Benchmark is loopback-only"
results = []
for case in config["cases"]:
    row = {"items": case["items"], "samples": []}
    for index in range(-1, config["repetitions"]):
        job_id = "bench-" + uuid.uuid4().hex
        started = time.perf_counter_ns()
        payload = json.dumps({"version": 1, "job_id": job_id,
                              "printer_id": config["printer_id"],
                              "return_png": True, "data": case["data"]},
                             ensure_ascii=False, separators=(",", ":")).encode()
        connection = http.client.HTTPConnection(url.hostname, url.port, timeout=60)
        connection.request("POST", "/v1/print-jobs", payload, {
            "Authorization": "Bearer " + config["api_key"],
            "Content-Type": "application/json"})
        response = connection.getresponse()
        raw = response.read()
        elapsed = (time.perf_counter_ns() - started) / 1_000_000
        connection.close()
        # Parsing and verification happen after the response timing stops.
        body = json.loads(raw)
        sample = {"ms": elapsed, "status": response.status,
                  "request_bytes": len(payload), "response_bytes": len(raw)}
        if response.status == 200:
            assert body["id"] == job_id and body["duplicate"] is False
            png = body["png"]
            decoded = base64.b64decode(png["data"], validate=True)
            assert decoded.startswith(b"\x89PNG\r\n\x1a\n")
            sample.update(png_bytes=len(decoded), width=png["width"], height=png["height"])
        else:
            sample["error"] = body.get("error", str(body))
        if index == -1:
            row["warmup"] = sample
        else:
            row["samples"].append(sample)
    results.append(row)
    print(f"HTTP {case['items']} items: {row['samples'][0]['ms']:.1f} ms, "
          f"status={row['samples'][0]['status']}", file=sys.stderr, flush=True)
json.dump(results, sys.stdout, ensure_ascii=False, indent=2)
sys.stdout.write("\n")
