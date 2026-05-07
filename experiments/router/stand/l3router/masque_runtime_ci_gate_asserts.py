#!/usr/bin/env python3
import argparse
import json
import os
import subprocess
import sys
from pathlib import Path


MANDATORY_CHECKS = (
    ("runtime_single_source_drift", "ok", True),
    ("runtime_artifacts_error_source", "ok", True),
    ("anti_bypass_contract", "ok", True),
    ("anti_bypass_contract", "rows.tcp_stream.ok", True),
    ("anti_bypass_contract", "rows.udp.ok", True),
    ("anti_bypass_contract", "rows.tcp_ip.ok", True),
    ("anti_bypass_contract", "parity_with_summary.ok", True),
    ("runtime_artifacts", "malformed_scoped_transport.ok", True),
    ("runtime_artifacts", "malformed_scoped_boundary_parity.ok", True),
    ("summary", "connect_ip_post_send_remote_visibility_correlation.ok", True),
)
REQUIRED_SCHEMA = "masque_runtime_contract"
REQUIRED_TOP_LEVEL_FIELDS = ("ok", "runtime_dir", "checks", "failures")
NIGHTLY_REQUIRED_SUMMARIES = (
    "runtime/nightly_smoke_summary.json",
    "runtime/nightly_real_perf_summary.json",
    "runtime/nightly_tcp_ip_stress_summary.json",
)
ANTI_BYPASS_SCHEMA = "masque_anti_bypass_contract"
ANTI_BYPASS_SCHEMA_VERSION = 1
ANTI_BYPASS_ALLOWED_ERROR_SOURCES = {"runtime", "compose_up", "helper", "none"}
ANTI_BYPASS_MODE_TO_SCENARIO = {
    "tcp_stream": "tcp_stream",
    "udp": "udp",
    "tcp_ip": "tcp_ip",
}
ANTI_BYPASS_MODE_TO_CLIENT_CONFIG = {
    "tcp_stream": "./configs/masque-client.json",
    "udp": "./configs/masque-client.json",
    "tcp_ip": "./configs/masque-client-connect-ip.json",
}
ANTI_BYPASS_PARITY_ROW_MODES = ("tcp_stream", "udp", "tcp_ip")
ANTI_BYPASS_SCENARIO_SET = frozenset(ANTI_BYPASS_MODE_TO_SCENARIO.values())
CONNECT_IP_BRIDGE_ALLOWED_REASON_BUCKETS = frozenset(
    ("datagram_too_large", "non_ptb_write_fail", "ptb_feedback_err")
)
CONNECT_STREAM_NON_AUTH_MATRIX_TESTS = (
    "TestDialTCPStreamNonAuthStatusMapsToDialClass",
    "TestDialTCPStreamInProcessHTTP3ProxyNonAuthStatusMapsToDialClass",
)
SCOPED_PARITY_ROWS = (
    ("scoped_cross_artifact_parity", "negative_peer_abort_strict_ok", True),
    ("scoped_cross_artifact_parity", "negative_peer_invalid_route_advertisement_strict_ok", True),
)
RUNTIME_ERROR_SOURCE_ROWS = (
    ("runtime_artifacts_error_source", "rows.peer_abort.ok", True),
    ("runtime_artifacts_error_source", "rows.route_advertise.ok", True),
)


def _normalize_anti_bypass_error_source(value: object) -> str:
    source = str(value or "").strip().lower()
    if not source:
        return "runtime"
    if source not in ANTI_BYPASS_ALLOWED_ERROR_SOURCES:
        return "runtime"
    return source


def _load_json(path: Path) -> dict:
    if not path.exists():
        raise RuntimeError(f"artifact missing: {path}")
    try:
        data = json.loads(path.read_text(encoding="utf-8"))
    except Exception as exc:  # pragma: no cover - defensive parse path
        raise RuntimeError(f"artifact invalid_json: {path} ({exc})") from exc
    if not isinstance(data, dict):
        raise RuntimeError(f"artifact payload must be object: {path}")
    return data


def _lookup(check: dict, dotted_key: str):
    cur = check
    for part in dotted_key.split("."):
        if not isinstance(cur, dict) or part not in cur:
            return None
        cur = cur[part]
    return cur


def _assert_expected(checks: dict, check_name: str, field: str, expected) -> str | None:
    check = checks.get(check_name)
    if not isinstance(check, dict):
        return f"missing runtime check: checks.{check_name}"
    actual = _lookup(check, field)
    if actual is expected:
        return None
    return f"gate failed: checks.{check_name}.{field}={actual!r} expected={expected!r}"


def _assert_runtime_contract_anti_bypass_parity_rows(checks: dict) -> list[str]:
    failures: list[str] = []
    check = checks.get("anti_bypass_contract")
    if not isinstance(check, dict):
        return ["missing runtime check: checks.anti_bypass_contract"]
    parity = check.get("parity_with_summary")
    if not isinstance(parity, dict):
        return ["missing runtime check: checks.anti_bypass_contract.parity_with_summary"]
    rows = parity.get("rows")
    if not isinstance(rows, dict):
        return ["missing runtime check: checks.anti_bypass_contract.parity_with_summary.rows"]
    for mode in ANTI_BYPASS_PARITY_ROW_MODES:
        row = rows.get(mode)
        if not isinstance(row, dict):
            failures.append(
                f"missing runtime check: checks.anti_bypass_contract.parity_with_summary.rows.{mode}"
            )
            continue
        if row.get("ok") is not True:
            failures.append(
                "gate failed: "
                f"checks.anti_bypass_contract.parity_with_summary.rows.{mode}.ok={row.get('ok')!r} expected=True"
            )
    return failures


def _assert_runtime_contract_scoped_parity_rows(checks: dict) -> list[str]:
    failures: list[str] = []
    for check_name, field, expected in (*RUNTIME_ERROR_SOURCE_ROWS, *SCOPED_PARITY_ROWS):
        failure = _assert_expected(checks, check_name, field, expected)
        if failure:
            failures.append(failure)
    return failures


def _assert_runtime_contract_connect_ip_bridge_reason_parity(checks: dict) -> list[str]:
    failures: list[str] = []
    summary = checks.get("summary")
    if not isinstance(summary, dict):
        return ["missing runtime check: checks.summary"]
    row = summary.get("connect_ip_bridge_write_err_reason_parity")
    if not isinstance(row, dict):
        return ["missing runtime check: checks.summary.connect_ip_bridge_write_err_reason_parity"]
    if row.get("ok") is not True:
        failures.append(
            "gate failed: "
            "checks.summary.connect_ip_bridge_write_err_reason_parity.ok="
            f"{row.get('ok')!r} expected=True"
        )
    unknown = row.get("unknown_reason_buckets")
    if isinstance(unknown, list) and unknown:
        failures.append(
            "gate failed: checks.summary.connect_ip_bridge_write_err_reason_parity.unknown_reason_buckets="
            f"{unknown!r} expected=[]"
        )
    allowed = row.get("allowed_reason_buckets")
    if isinstance(allowed, list):
        expected = sorted(CONNECT_IP_BRIDGE_ALLOWED_REASON_BUCKETS)
        if sorted(str(item) for item in allowed) != expected:
            failures.append(
                "gate failed: checks.summary.connect_ip_bridge_write_err_reason_parity.allowed_reason_buckets="
                f"{allowed!r} expected={expected!r}"
            )
    else:
        failures.append(
            "gate failed: checks.summary.connect_ip_bridge_write_err_reason_parity.allowed_reason_buckets missing/not-list"
        )
    return failures


def _assert_runtime_contract_connect_ip_post_send_remote_visibility_correlation(checks: dict) -> list[str]:
    failures: list[str] = []
    summary = checks.get("summary")
    if not isinstance(summary, dict):
        return ["missing runtime check: checks.summary"]
    row = summary.get("connect_ip_post_send_remote_visibility_correlation")
    if not isinstance(row, dict):
        return ["missing runtime check: checks.summary.connect_ip_post_send_remote_visibility_correlation"]
    # Debug gate is strict only when branch is active in the artifact.
    # For green/non-strict artifacts active=false is expected and should not fail this point-check.
    if row.get("active") is not True:
        return failures
    stop_reason = str(row.get("stop_reason", "")).strip().lower()
    if stop_reason == "none" and row.get("ok") is True:
        return failures
    if row.get("ok") is not True:
        failures.append(
            "gate failed: "
            "checks.summary.connect_ip_post_send_remote_visibility_correlation.ok="
            f"{row.get('ok')!r} expected=True"
        )
    if stop_reason != "post_send_frame_visibility_absent":
        failures.append(
            "gate failed: "
            "checks.summary.connect_ip_post_send_remote_visibility_correlation.stop_reason="
            f"{row.get('stop_reason')!r} expected='post_send_frame_visibility_absent'"
        )
    return failures


def _assert_contract_schema(payload: dict) -> list[str]:
    failures: list[str] = []
    schema = str(payload.get("schema", "")).strip()
    if schema != REQUIRED_SCHEMA:
        failures.append(f"schema gate failed: schema={payload.get('schema')!r} expected={REQUIRED_SCHEMA!r}")
    if not isinstance(payload.get("schema_version"), int):
        failures.append(
            f"schema gate failed: schema_version type={type(payload.get('schema_version')).__name__!r} expected='int'"
        )
    for key in REQUIRED_TOP_LEVEL_FIELDS:
        if key not in payload:
            failures.append(f"schema gate failed: missing required top-level field={key!r}")
    if "checks" in payload and not isinstance(payload.get("checks"), dict):
        failures.append("schema gate failed: checks must be object")
    if "failures" in payload and not isinstance(payload.get("failures"), list):
        failures.append("schema gate failed: failures must be list")
    return failures


def _assert_connect_ip_negative_control(payload: dict) -> list[str]:
    failures: list[str] = []
    if str(payload.get("ok", "true")).lower() != "false":
        failures.append(f"negative control expected summary.ok=false, got={payload.get('ok')!r}")
    results = payload.get("results")
    if not isinstance(results, list):
        failures.append("negative control expected summary.results array")
        return failures
    tcp_ip = next((row for row in results if isinstance(row, dict) and row.get("scenario") == "tcp_ip"), None)
    if not isinstance(tcp_ip, dict):
        failures.append("negative control expected tcp_ip row in summary.results")
        return failures
    if bool(tcp_ip.get("ok")):
        failures.append("negative control expected tcp_ip.ok=false")
    error_class = str(tcp_ip.get("error_class") or "").strip().lower()
    if error_class in {"", "none"}:
        failures.append(f"negative control expected classified error_class, got={tcp_ip.get('error_class')!r}")
    return failures


def _assert_nightly_perf_thresholds(paths: list[Path]) -> list[str]:
    failures: list[str] = []
    loaded_payloads: dict[Path, dict] = {}
    for path in paths:
        label = str(path)
        if not path.exists():
            failures.append(f"{label}: missing")
            continue
        try:
            payload = json.loads(path.read_text(encoding="utf-8"))
        except Exception as exc:  # pragma: no cover - defensive parse path
            failures.append(f"{label}: invalid_json={exc}")
            continue
        if not isinstance(payload, dict):
            failures.append(f"{label}: payload must be object")
            continue
        loaded_payloads[path] = payload
        if str(payload.get("ok", "false")).lower() != "true":
            failures.append(f"{label}: ok={payload.get('ok')!r}")
        results = payload.get("results")
        if not isinstance(results, list) or not results:
            failures.append(f"{label}: results missing/empty")

    real_summary_path = next((path for path in paths if path.name == "nightly_real_perf_summary.json"), None)
    if real_summary_path and real_summary_path in loaded_payloads:
        real_summary = loaded_payloads[real_summary_path]
        results = real_summary.get("results") or []
        if not results:
            failures.append(f"{real_summary_path}: no results")
        else:
            first = results[0]
            if not isinstance(first, dict):
                failures.append(f"{real_summary_path}: first result must be object")
            else:
                if first.get("scenario") != "tcp_ip_iperf":
                    failures.append(f"{real_summary_path}: scenario mismatch={first.get('scenario')!r}")
                if int(first.get("stable_trial_count", 0) or 0) < 1:
                    failures.append(f"{real_summary_path}: stable_trial_count < 1")
                highest = first.get("highest_stable")
                if not isinstance(highest, dict):
                    failures.append(f"{real_summary_path}: highest_stable missing")
                elif float(highest.get("receiver_mbps", 0) or 0) <= 0:
                    failures.append(
                        f"{real_summary_path}: highest_stable.receiver_mbps={highest.get('receiver_mbps')!r}"
                    )
    return failures


def _run_cmd(command: list[str], cwd: Path, env: dict[str, str] | None = None, check: bool = True) -> subprocess.CompletedProcess:
    return subprocess.run(command, cwd=str(cwd), env=env, text=True, capture_output=True, check=check)


def _docker_masque_public_network_name(docker_bin: str) -> str | None:
    proc = subprocess.run(
        [
            docker_bin,
            "inspect",
            "-f",
            "{{json .NetworkSettings.Networks}}",
            "masque-server-core",
        ],
        capture_output=True,
        text=True,
        check=False,
    )
    if proc.returncode != 0 or not (proc.stdout or "").strip():
        return None
    try:
        nets = json.loads(proc.stdout.strip())
    except json.JSONDecodeError:
        return None
    if not isinstance(nets, dict):
        return None
    for name in nets:
        if name == "masque-public" or str(name).endswith("_masque-public"):
            return str(name)
    return None


def _assert_negative_control_from_summary(payload: dict, scenario: str) -> list[str]:
    failures: list[str] = []
    if str(payload.get("ok", "true")).lower() != "false":
        failures.append(f"{scenario}: expected summary.ok=false, got={payload.get('ok')!r}")
    rows = payload.get("results")
    if not isinstance(rows, list):
        failures.append(f"{scenario}: expected summary.results array")
        return failures
    row = next((item for item in rows if isinstance(item, dict) and item.get("scenario") == scenario), None)
    if not isinstance(row, dict):
        failures.append(f"{scenario}: expected row in summary.results")
        return failures
    if bool(row.get("ok")):
        failures.append(f"{scenario}: expected scenario.ok=false")
    error_class = str(row.get("error_class") or "").strip().lower()
    if error_class in {"", "none"}:
        failures.append(f"{scenario}: expected classified error_class, got={row.get('error_class')!r}")
    return failures


def _classify_mode_row(summary: dict, scenario: str, mode: str, returncode: int) -> dict:
    row_failures = _assert_negative_control_from_summary(summary, scenario)
    scenario_row = next(
        (item for item in summary.get("results", []) if isinstance(item, dict) and item.get("scenario") == scenario),
        None,
    )
    error_class = ""
    if isinstance(scenario_row, dict):
        error_class = str(scenario_row.get("error_class") or "").strip().lower()
    if error_class in {"", "none"}:
        error_class = "unknown"
    error_source = _normalize_anti_bypass_error_source((scenario_row or {}).get("error_source"))
    return {
        "mode": mode,
        "scenario": scenario,
        "ok": not row_failures,
        "summary_ok": summary.get("ok"),
        "runner_exit_code": int(returncode),
        "error_class": error_class,
        "error_source": error_source,
        "failures": row_failures,
    }


def _build_anti_bypass_artifact(mode_rows: list[dict]) -> dict:
    failures: list[str] = []
    for row in mode_rows:
        for failure in row.get("failures", []):
            failures.append(f"{row.get('mode')}: {failure}")
    return {
        "schema": ANTI_BYPASS_SCHEMA,
        "schema_version": ANTI_BYPASS_SCHEMA_VERSION,
        "ok": not failures,
        "modes": mode_rows,
        "failures": failures,
    }


def _write_json(path: Path, payload: dict) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(payload, ensure_ascii=True, indent=2, sort_keys=True) + "\n", encoding="utf-8")


def _assert_connect_stream_non_auth_status_matrix(workdir: Path) -> list[str]:
    failures: list[str] = []
    go_pkg = "./transport/masque"
    test_run = "|".join(CONNECT_STREAM_NON_AUTH_MATRIX_TESTS)
    command = [
        "go",
        "test",
        go_pkg,
        "-tags",
        "with_masque",
        "-count=1",
        "-run",
        test_run,
    ]
    result = _run_cmd(command, cwd=workdir, env=os.environ.copy(), check=False)
    if result.returncode != 0:
        stderr = (result.stderr or "").strip()
        stdout = (result.stdout or "").strip()
        details = stderr or stdout or "<no output>"
        failures.append(
            "connect-stream non-auth status matrix go test failed: "
            f"rc={result.returncode} details={details}"
        )
    return failures


def _green_tcp_ip_from_tcp_ip_scoped(rows: list) -> dict | None:
    for item in rows:
        if not isinstance(item, dict) or item.get("scenario") != "tcp_ip_scoped":
            continue
        nested = item.get("rows")
        if not isinstance(nested, list):
            continue
        for row in nested:
            if not isinstance(row, dict) or row.get("kind") != "positive":
                continue
            res = row.get("result")
            if isinstance(res, dict) and res.get("scenario") == "tcp_ip" and bool(res.get("ok")):
                return res
    return None


def _merge_post_anti_bypass_summary(backup_text: str, negative_by_scenario: dict[str, dict]) -> dict:
    data = json.loads(backup_text)
    raw = [x for x in data.get("results", []) if isinstance(x, dict)]
    preserved_green_tcp_ip = next(
        (
            x
            for x in raw
            if x.get("scenario") == "tcp_ip"
            and bool(x.get("ok"))
            and str(x.get("error_class") or "").strip().lower() in {"none", ""}
        ),
        None,
    )
    stripped = [x for x in raw if x.get("scenario") not in ANTI_BYPASS_SCENARIO_SET]
    has_green_flat = any(
        isinstance(x, dict) and x.get("scenario") == "tcp_ip" and bool(x.get("ok")) for x in stripped
    )
    merged: list[dict] = list(stripped)
    if not has_green_flat:
        injected = preserved_green_tcp_ip or _green_tcp_ip_from_tcp_ip_scoped(stripped)
        if isinstance(injected, dict):
            merged.append(injected)
    for scen in ("tcp_stream", "udp"):
        picked = negative_by_scenario.get(scen)
        if isinstance(picked, dict):
            merged.append(picked)
    tip = negative_by_scenario.get("tcp_ip")
    if isinstance(tip, dict):
        merged.append(tip)
    data["results"] = merged
    non_anti = [
        x
        for x in merged
        if isinstance(x, dict) and x.get("scenario") not in ANTI_BYPASS_SCENARIO_SET
    ]
    # Вакуумное all([])==True недопустимо: без строк из «основной» матрицы summary не имеет права быть зелёным.
    data["ok"] = bool(non_anti) and all(bool(x.get("ok")) for x in non_anti)
    return data


def _run_anti_bypass_negative_control(modes: list[str], workdir: Path, docker_bin: str, artifact_path: Path) -> list[str]:
    failures: list[str] = []
    compose_file = "docker-compose.masque-e2e.yml"
    runner = "masque_stand_runner.py"
    summary_path = workdir / "runtime" / "masque_python_runner_summary.json"
    summary_backup = summary_path.read_text(encoding="utf-8") if summary_path.exists() else None
    mode_rows: list[dict] = []
    negative_by_scenario: dict[str, dict] = {}
    try:
        for mode in modes:
            scenario = ANTI_BYPASS_MODE_TO_SCENARIO[mode]
            client_config = ANTI_BYPASS_MODE_TO_CLIENT_CONFIG[mode]
            env = os.environ.copy()
            env["MASQUE_CLIENT_CONFIG"] = client_config
            env["MASQUE_STAND_SKIP_COMPOSE_UP"] = "1"
            env["MASQUE_STAND_SKIP_SMOKE_CONTRACT_FILES"] = "1"
            try:
                _run_cmd([docker_bin, "compose", "-f", compose_file, "down", "-v"], cwd=workdir, env=env, check=False)
                _run_cmd([docker_bin, "compose", "-f", compose_file, "up", "-d", "--build"], cwd=workdir, env=env)
                pub_net = _docker_masque_public_network_name(docker_bin)
                if not pub_net:
                    failures.append(f"{mode}: cannot resolve masque-public Docker network on masque-server-core")
                    mode_rows.append(
                        {
                            "mode": mode,
                            "scenario": scenario,
                            "ok": False,
                            "summary_ok": None,
                            "runner_exit_code": -1,
                            "error_class": "unknown",
                            "error_source": "helper",
                            "failures": ["cannot resolve masque-public Docker network on masque-server-core"],
                        }
                    )
                    continue
                disconnect = _run_cmd(
                    [docker_bin, "network", "disconnect", "-f", pub_net, "masque-server-core"],
                    cwd=workdir,
                    env=env,
                    check=False,
                )
                if disconnect.returncode != 0:
                    stderr = (disconnect.stderr or "").strip()
                    failures.append(f"{mode}: docker network disconnect {pub_net!r}: rc={disconnect.returncode} stderr={stderr!r}")
                    mode_rows.append(
                        {
                            "mode": mode,
                            "scenario": scenario,
                            "ok": False,
                            "summary_ok": None,
                            "runner_exit_code": -1,
                            "error_class": "unknown",
                            "error_source": "helper",
                            "failures": [
                                f"docker network disconnect rc={disconnect.returncode} stderr={stderr!r}"
                            ],
                        }
                    )
                    continue
                run_res = _run_cmd([sys.executable, runner, "--scenario", scenario], cwd=workdir, env=env, check=False)
                if run_res.returncode == 0:
                    failures.append(f"{mode}: runner unexpectedly succeeded with MASQUE server down")
                    mode_rows.append(
                        {
                            "mode": mode,
                            "scenario": scenario,
                            "ok": False,
                            "summary_ok": None,
                            "runner_exit_code": int(run_res.returncode),
                            "error_class": "unknown",
                            "error_source": "helper",
                            "failures": ["runner unexpectedly succeeded with MASQUE server down"],
                        }
                    )
                    continue
                try:
                    summary = _load_json(summary_path)
                except RuntimeError as exc:
                    failures.append(f"{mode}: {exc}")
                    mode_rows.append(
                        {
                            "mode": mode,
                            "scenario": scenario,
                            "ok": False,
                            "summary_ok": None,
                            "runner_exit_code": int(run_res.returncode),
                            "error_class": "unknown",
                            "error_source": "helper",
                            "failures": [str(exc)],
                        }
                    )
                    continue
                row = _classify_mode_row(summary=summary, scenario=scenario, mode=mode, returncode=run_res.returncode)
                mode_rows.append(row)
                failures.extend(row.get("failures", []))
                picked = next(
                    (
                        r
                        for r in summary.get("results", [])
                        if isinstance(r, dict) and r.get("scenario") == scenario
                    ),
                    None,
                )
                if isinstance(picked, dict):
                    negative_by_scenario[scenario] = picked
                if mode == "tcp_ip":
                    failures.extend(_assert_connect_ip_negative_control(summary))
            finally:
                _run_cmd([docker_bin, "compose", "-f", compose_file, "down", "-v"], cwd=workdir, env=env, check=False)
    finally:
        if summary_backup is not None:
            summary_path.parent.mkdir(parents=True, exist_ok=True)
            try:
                merged = _merge_post_anti_bypass_summary(summary_backup, negative_by_scenario)
                summary_path.write_text(json.dumps(merged, indent=2, ensure_ascii=False) + "\n", encoding="utf-8")
            except (json.JSONDecodeError, KeyError, TypeError, ValueError):
                summary_path.write_text(summary_backup, encoding="utf-8")
    artifact = _build_anti_bypass_artifact(mode_rows)
    _write_json(artifact_path, artifact)
    return failures


def main() -> int:
    parser = argparse.ArgumentParser(description="Assert mandatory MASQUE runtime CI gates from aggregated contract")
    parser.add_argument(
        "--contract",
        type=Path,
        default=Path("runtime/masque_runtime_contract_latest.json"),
        help="Path to aggregated runtime contract artifact",
    )
    parser.add_argument(
        "--assert-schema",
        action="store_true",
        help="Enable blocking schema/top-level shape checks for aggregated contract artifact",
    )
    parser.add_argument(
        "--assert-anti-bypass-parity-rows",
        action="store_true",
        help="Assert strict per-row anti-bypass parity rows from aggregated runtime contract.",
    )
    parser.add_argument(
        "--assert-scoped-parity-rows",
        action="store_true",
        help=(
            "Assert strict scoped/lifecycle per-row gates from aggregated runtime contract "
            "(route_advertise, peer_abort, scoped parity rows)."
        ),
    )
    parser.add_argument(
        "--assert-connect-ip-bridge-reason-parity",
        action="store_true",
        help=(
            "Assert typed CONNECT-IP bridge write-error reason parity rows "
            "from aggregated runtime contract."
        ),
    )
    parser.add_argument(
        "--assert-connect-ip-post-send-remote-visibility-correlation",
        action="store_true",
        help=(
            "Assert typed post-send remote-visibility correlation row "
            "from aggregated runtime contract."
        ),
    )
    parser.add_argument(
        "--assert-connect-ip-negative-control",
        type=Path,
        default=None,
        help="Assert anti-bypass contract from MASQUE runner summary artifact",
    )
    parser.add_argument(
        "--assert-nightly-perf-thresholds",
        action="store_true",
        help="Assert nightly perf summary thresholds from JSON artifacts",
    )
    parser.add_argument(
        "--assert-connect-stream-non-auth-status-matrix",
        action="store_true",
        help=(
            "Run typed pre-docker guard for CONNECT-STREAM non-auth status matrix "
            "(stub + in-process tests)."
        ),
    )
    parser.add_argument(
        "--run-anti-bypass-negative-control",
        action="append",
        choices=tuple(ANTI_BYPASS_MODE_TO_SCENARIO.keys()),
        default=None,
        help="Run anti-bypass negative control for selected modes (repeat flag).",
    )
    parser.add_argument(
        "--anti-bypass-artifact",
        type=Path,
        default=Path("runtime/anti_bypass_latest.json"),
        help="Path for typed anti-bypass artifact export.",
    )
    parser.add_argument(
        "--workdir",
        type=Path,
        default=Path("."),
        help="Working directory with stand runner and docker-compose file.",
    )
    parser.add_argument(
        "--docker-bin",
        type=str,
        default="docker",
        help="Docker CLI binary used by anti-bypass helper.",
    )
    parser.add_argument(
        "--nightly-summary",
        action="append",
        type=Path,
        default=None,
        help="Nightly summary artifact path (repeat flag). Defaults to required summaries in runtime/",
    )
    args = parser.parse_args()

    failures = []

    if args.assert_connect_ip_negative_control:
        try:
            payload = _load_json(args.assert_connect_ip_negative_control.resolve())
        except RuntimeError as exc:
            print(str(exc))
            return 1
        failures.extend(_assert_connect_ip_negative_control(payload))
        if failures:
            print("MASQUE CONNECT-IP negative control gate failures:")
            for failure in failures:
                print(" -", failure)
            return 1
        print("MASQUE CONNECT-IP negative control gate passed.")
        return 0

    if args.assert_nightly_perf_thresholds:
        summary_paths = args.nightly_summary or [Path(path) for path in NIGHTLY_REQUIRED_SUMMARIES]
        resolved = [path.resolve() for path in summary_paths]
        failures.extend(_assert_nightly_perf_thresholds(resolved))
        if failures:
            print("MASQUE nightly perf threshold failures:")
            for failure in failures:
                print(" -", failure)
            return 1
        print("MASQUE nightly perf thresholds passed.")
        return 0

    if args.assert_connect_stream_non_auth_status_matrix:
        workdir = args.workdir.resolve()
        failures.extend(_assert_connect_stream_non_auth_status_matrix(workdir))
        if failures:
            print("MASQUE CONNECT-STREAM non-auth status matrix gate failures:")
            for failure in failures:
                print(" -", failure)
            return 1
        print("MASQUE CONNECT-STREAM non-auth status matrix gate passed.")
        return 0

    if args.run_anti_bypass_negative_control:
        failures.extend(
            _run_anti_bypass_negative_control(
                modes=args.run_anti_bypass_negative_control,
                workdir=args.workdir.resolve(),
                docker_bin=args.docker_bin,
                artifact_path=args.anti_bypass_artifact.resolve(),
            )
        )
        if failures:
            print("MASQUE anti-bypass negative control failures:")
            for failure in failures:
                print(" -", failure)
            return 1
        print("MASQUE anti-bypass negative controls passed.")
        return 0

    try:
        payload = _load_json(args.contract.resolve())
    except RuntimeError as exc:
        print(str(exc))
        return 1

    checks = payload.get("checks")
    if not isinstance(checks, dict):
        print("invalid contract payload: checks must be object")
        return 1

    if args.assert_anti_bypass_parity_rows:
        failures.extend(_assert_runtime_contract_anti_bypass_parity_rows(checks))
        if failures:
            print("MASQUE runtime anti-bypass parity row gate failures:")
            for failure in failures:
                print(" -", failure)
            return 1
        print("MASQUE runtime anti-bypass parity row gate passed.")
        return 0

    if args.assert_scoped_parity_rows:
        failures.extend(_assert_runtime_contract_scoped_parity_rows(checks))
        if failures:
            print("MASQUE runtime scoped/lifecycle parity row gate failures:")
            for failure in failures:
                print(" -", failure)
            return 1
        print("MASQUE runtime scoped/lifecycle parity row gate passed.")
        return 0

    if args.assert_connect_ip_bridge_reason_parity:
        failures.extend(_assert_runtime_contract_connect_ip_bridge_reason_parity(checks))
        if failures:
            print("MASQUE runtime CONNECT-IP bridge reason parity gate failures:")
            for failure in failures:
                print(" -", failure)
            return 1
        print("MASQUE runtime CONNECT-IP bridge reason parity gate passed.")
        return 0

    if args.assert_connect_ip_post_send_remote_visibility_correlation:
        failures.extend(_assert_runtime_contract_connect_ip_post_send_remote_visibility_correlation(checks))
        if failures:
            print("MASQUE runtime CONNECT-IP post-send remote-visibility correlation gate failures:")
            for failure in failures:
                print(" -", failure)
            return 1
        print("MASQUE runtime CONNECT-IP post-send remote-visibility correlation gate passed.")
        return 0

    if args.assert_schema:
        failures.extend(_assert_contract_schema(payload))
    for check_name, field, expected in MANDATORY_CHECKS:
        failure = _assert_expected(checks, check_name, field, expected)
        if failure:
            failures.append(failure)

    if failures:
        print("MASQUE runtime CI mandatory gate failures:")
        for failure in failures:
            print(" -", failure)
        return 1

    print("MASQUE runtime CI mandatory gate asserts passed.")
    return 0


if __name__ == "__main__":
    sys.exit(main())
