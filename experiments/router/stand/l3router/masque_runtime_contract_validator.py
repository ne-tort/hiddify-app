#!/usr/bin/env python3
import argparse
import json
import sys
from pathlib import Path


RUNTIME_DIR_DEFAULT = Path(__file__).resolve().parent / "runtime"
CONTRACT_SCHEMA = "masque_runtime_contract"
CONTRACT_SCHEMA_VERSION = 1
REQUIRED_TOP_LEVEL_KEYS = {"schema", "schema_version", "ok", "runtime_dir", "checks", "failures"}
SCOPED_CLASS_ALLOWED = {"capability", "policy"}
SCOPED_SOURCE_ALLOWED = {"runtime", "compose_up"}
ANTI_BYPASS_SCHEMA = "masque_anti_bypass_contract"
ANTI_BYPASS_SCHEMA_VERSION = 1
ANTI_BYPASS_ALLOWED_ERROR_SOURCES = {"runtime", "compose_up", "helper", "none"}
CONNECT_IP_BRIDGE_REQUIRED_DELTA_KEYS = (
    "connect_ip_bridge_build_total",
    "connect_ip_bridge_write_enter_total",
    "connect_ip_bridge_write_ok_total",
    "connect_ip_bridge_write_err_total",
    "connect_ip_bridge_read_enter_total",
    "connect_ip_bridge_read_exit_total",
    "connect_ip_bridge_read_exit_err_total",
    "connect_ip_bridge_readpacket_enter_total",
    "connect_ip_bridge_readpacket_return_total",
    "connect_ip_bridge_readpacket_err_total",
    "connect_ip_bridge_readpacket_timeout_total",
    "connect_ip_bridge_readpacket_return_path_total",
    "connect_ip_bridge_write_ok_to_read_enter_ms",
    "connect_ip_bridge_read_enter_to_read_exit_ms",
    "connect_ip_bridge_write_err_reason_total",
    "connect_ip_receive_datagram_wait_total",
    "connect_ip_receive_datagram_wait_err_total",
    "connect_ip_receive_datagram_wait_closed_total",
    "connect_ip_receive_datagram_wake_total",
    "connect_ip_receive_datagram_wait_duration_total_ms",
    "connect_ip_receive_datagram_wait_duration_max_ms",
    "connect_ip_receive_datagram_last_wait_start_unix_milli",
    "connect_ip_receive_datagram_last_wake_unix_milli",
    "connect_ip_receive_datagram_close_cancel_enter_total",
    "connect_ip_receive_datagram_close_cancel_fired_total",
    "connect_ip_receive_datagram_close_cancel_return_ok_total",
    "connect_ip_receive_datagram_close_cancel_return_err_total",
    "connect_ip_receive_datagram_return_total",
    "connect_ip_receive_datagram_return_path_total",
    "connect_ip_receive_datagram_post_return_total",
    "connect_ip_receive_datagram_post_return_path_total",
    "connect_ip_engine_pmtu_update_total",
    "connect_ip_engine_pmtu_update_reason_total",
    "connect_ip_proxied_packet_drop_total",
    "connect_ip_proxied_packet_drop_reason_total",
    "http3_stream_datagram_queue_pop_total",
    "http3_stream_datagram_queue_pop_path_total",
    "http3_datagram_dispatch_path_total",
    "http3_datagram_receive_wait_path_total",
    "quic_datagram_receive_wait_path_total",
    "quic_packet_receive_drop_path_total",
    "quic_packet_receive_ingress_path_total",
    "quic_datagram_post_decrypt_path_total",
    "quic_datagram_send_path_total",
    "quic_datagram_send_pipeline_path_total",
    "quic_datagram_send_write_path_total",
    "quic_datagram_tx_path_total",
    "quic_datagram_tx_packet_len_total",
    "quic_datagram_pre_ingress_path_total",
    "quic_datagram_ingress_path_total",
    "quic_datagram_rcv_queue_pop_total",
    "quic_datagram_rcv_queue_pop_path_total",
)
CONNECT_IP_BRIDGE_ALLOWED_REASON_BUCKETS = {
    "datagram_too_large",
    "non_ptb_write_fail",
    "ptb_feedback_err",
}
ANTI_BYPASS_EXPECTED_MODES = {
    "tcp_stream": "tcp_stream",
    "udp": "udp",
    "tcp_ip": "tcp_ip",
}
LEGACY_ADHOC_CHECK_KEYS = {
    "peer_abort_lifecycle_gate",
    "route_advertise_dual_signal_gate",
    "peer_abort_gate",
    "route_advertise_gate",
}
SINGLE_SOURCE_REQUIRED_ARTIFACT_ROWS = ("peer_abort", "route_advertise")


def _load_json(path: Path, failures: list, label: str):
    if not path.exists():
        failures.append(f"{label}: missing")
        return None
    try:
        return json.loads(path.read_text(encoding="utf-8"))
    except Exception as exc:
        failures.append(f"{label}: invalid_json={exc}")
        return None


def _as_int(value, default=0):
    try:
        return int(value)
    except (TypeError, ValueError):
        return default


def _normalize_anti_bypass_error_source(value) -> str:
    source = str(value or "").strip().lower()
    if not source:
        return "runtime"
    if source not in ANTI_BYPASS_ALLOWED_ERROR_SOURCES:
        return "runtime"
    return source


def _check_output_schema_compatibility(output: Path, failures: list):
    if not output.exists():
        return
    existing = _load_json(output, failures, "output_contract")
    if not isinstance(existing, dict):
        return
    existing_schema = str(existing.get("schema", "")).strip()
    existing_version = _as_int(existing.get("schema_version", -1), -1)
    if existing_schema and existing_schema != CONTRACT_SCHEMA:
        failures.append(
            f"output_contract: schema={existing_schema} expected={CONTRACT_SCHEMA} (manual migration required)"
        )
    if existing_version > CONTRACT_SCHEMA_VERSION:
        failures.append(
            f"output_contract: schema_version={existing_version} newer_than_supported={CONTRACT_SCHEMA_VERSION}"
        )


def _check_payload_schema(payload: dict, failures: list):
    missing = sorted(REQUIRED_TOP_LEVEL_KEYS - set(payload.keys()))
    if missing:
        failures.append(f"output_contract: payload missing keys={missing}")
    if payload.get("schema") != CONTRACT_SCHEMA:
        failures.append(f"output_contract: payload.schema={payload.get('schema')} expected={CONTRACT_SCHEMA}")
    if _as_int(payload.get("schema_version", -1), -1) != CONTRACT_SCHEMA_VERSION:
        failures.append(
            f"output_contract: payload.schema_version={payload.get('schema_version')} expected={CONTRACT_SCHEMA_VERSION}"
        )
    if not isinstance(payload.get("checks"), dict):
        failures.append("output_contract: payload.checks must be object")
    if not isinstance(payload.get("failures"), list):
        failures.append("output_contract: payload.failures must be array")


def _check_smoke_summary(runtime_dir: Path, failures: list):
    summary = _load_json(runtime_dir / "masque_python_runner_summary.json", failures, "summary")
    if not isinstance(summary, dict):
        return {}

    if str(summary.get("ok", "false")).lower() != "true":
        failures.append(f"summary: ok={summary.get('ok')}")
    if not summary.get("results"):
        failures.append("summary: results missing/empty")

    tcp_ip = next((r for r in summary.get("results", []) if r.get("scenario") == "tcp_ip"), None)
    if tcp_ip is None:
        failures.append("summary: tcp_ip result missing")
        return {"summary_ok": False}

    bridge_contract = str(tcp_ip.get("connect_ip_udp_bridge_contract", "")).strip().lower()
    if bridge_contract != "ipv4_only":
        failures.append(
            f"summary: tcp_ip connect_ip_udp_bridge_contract={tcp_ip.get('connect_ip_udp_bridge_contract')} expected=ipv4_only"
        )
    if bool(tcp_ip.get("connect_ip_udp_bridge_ipv6_supported", True)):
        failures.append("summary: tcp_ip connect_ip_udp_bridge_ipv6_supported=true expected=false")

    obs_delta = (((tcp_ip.get("observability") or {}).get("delta")) or {})
    for key in CONNECT_IP_BRIDGE_REQUIRED_DELTA_KEYS:
        if key not in obs_delta:
            failures.append(f"summary: tcp_ip observability.delta missing required key={key}")

    unknown_reason_buckets = []
    bridge_write_reason_map = obs_delta.get("connect_ip_bridge_write_err_reason_total")
    if not isinstance(bridge_write_reason_map, dict):
        failures.append("summary: tcp_ip connect_ip_bridge_write_err_reason_total missing/not-object")
        bridge_write_reason_map = {}
    for key, value in bridge_write_reason_map.items():
        if str(key) not in CONNECT_IP_BRIDGE_ALLOWED_REASON_BUCKETS:
            unknown_reason_buckets.append(str(key))
            failures.append(
                "summary: tcp_ip connect_ip_bridge_write_err_reason_total "
                f"contains unsupported reason bucket={key!r}"
            )
        ivalue = _as_int(value, default=-1)
        if ivalue < 0:
            failures.append(
                "summary: tcp_ip connect_ip_bridge_write_err_reason_total "
                f"invalid value for bucket={key!r}: {value!r}"
            )
    icmp_reason = obs_delta.get("connect_ip_policy_drop_icmp_reason_total")
    if not isinstance(icmp_reason, dict):
        failures.append("summary: tcp_ip connect_ip_policy_drop_icmp_reason_total missing/not-object")
    else:
        for key in ("src_not_allowed", "dst_not_allowed", "proto_not_allowed"):
            value = icmp_reason.get(key, 0)
            ivalue = _as_int(value, default=-1)
            if ivalue < 0:
                failures.append(f"summary: tcp_ip connect_ip_policy_drop_icmp_reason_total[{key}] invalid={value}")
    proxied_drop_reason = obs_delta.get("connect_ip_proxied_packet_drop_reason_total")
    if not isinstance(proxied_drop_reason, dict):
        failures.append("summary: tcp_ip connect_ip_proxied_packet_drop_reason_total missing/not-object")
    else:
        for key in (
            "context_id_unknown",
            "malformed_datagram_quicvarint",
            "empty_packet",
            "packet_tuple_parse",
            "src_not_allowed",
            "dst_not_allowed",
            "proto_not_allowed",
        ):
            value = proxied_drop_reason.get(key, 0)
            ivalue = _as_int(value, default=-1)
            if ivalue < 0:
                failures.append(f"summary: tcp_ip connect_ip_proxied_packet_drop_reason_total[{key}] invalid={value}")
    return_path_reason = obs_delta.get("connect_ip_receive_datagram_return_path_total")
    if not isinstance(return_path_reason, dict):
        failures.append("summary: tcp_ip connect_ip_receive_datagram_return_path_total missing/not-object")
    else:
        for key in ("ok", "closed", "error"):
            value = return_path_reason.get(key, 0)
            ivalue = _as_int(value, default=-1)
            if ivalue < 0:
                failures.append(f"summary: tcp_ip connect_ip_receive_datagram_return_path_total[{key}] invalid={value}")
    post_return_path_reason = obs_delta.get("connect_ip_receive_datagram_post_return_path_total")
    if not isinstance(post_return_path_reason, dict):
        failures.append("summary: tcp_ip connect_ip_receive_datagram_post_return_path_total missing/not-object")
    else:
        for key in ("accepted", "context_id_unknown", "proxied_packet_drop", "malformed_datagram_quicvarint"):
            value = post_return_path_reason.get(key, 0)
            ivalue = _as_int(value, default=-1)
            if ivalue < 0:
                failures.append(f"summary: tcp_ip connect_ip_receive_datagram_post_return_path_total[{key}] invalid={value}")
    wait_duration_total_ms = _as_int(obs_delta.get("connect_ip_receive_datagram_wait_duration_total_ms", 0), default=-1)
    if wait_duration_total_ms < 0:
        failures.append("summary: tcp_ip connect_ip_receive_datagram_wait_duration_total_ms invalid")
    wait_duration_max_ms = _as_int(obs_delta.get("connect_ip_receive_datagram_wait_duration_max_ms", 0), default=-1)
    if wait_duration_max_ms < 0:
        failures.append("summary: tcp_ip connect_ip_receive_datagram_wait_duration_max_ms invalid")
    last_wait_start_ms = _as_int(obs_delta.get("connect_ip_receive_datagram_last_wait_start_unix_milli", 0), default=-1)
    if last_wait_start_ms < 0:
        failures.append("summary: tcp_ip connect_ip_receive_datagram_last_wait_start_unix_milli invalid")
    last_wake_ms = _as_int(obs_delta.get("connect_ip_receive_datagram_last_wake_unix_milli", 0), default=-1)
    if last_wake_ms < 0:
        failures.append("summary: tcp_ip connect_ip_receive_datagram_last_wake_unix_milli invalid")
    readpacket_path_reason = obs_delta.get("connect_ip_bridge_readpacket_return_path_total")
    if not isinstance(readpacket_path_reason, dict):
        failures.append("summary: tcp_ip connect_ip_bridge_readpacket_return_path_total missing/not-object")
    else:
        for key in ("ok", "timeout", "closed", "error"):
            value = readpacket_path_reason.get(key, 0)
            ivalue = _as_int(value, default=-1)
            if ivalue < 0:
                failures.append(f"summary: tcp_ip connect_ip_bridge_readpacket_return_path_total[{key}] invalid={value}")
    pmtu_update_reason = obs_delta.get("connect_ip_engine_pmtu_update_reason_total")
    if not isinstance(pmtu_update_reason, dict):
        failures.append("summary: tcp_ip connect_ip_engine_pmtu_update_reason_total missing/not-object")
    http3_queue_pop_path = obs_delta.get("http3_stream_datagram_queue_pop_path_total")
    if not isinstance(http3_queue_pop_path, dict):
        failures.append("summary: tcp_ip http3_stream_datagram_queue_pop_path_total missing/not-object")
    else:
        for key in ("queue_pop_ok", "queue_empty", "queue_closed", "queue_pop_err"):
            value = http3_queue_pop_path.get(key, 0)
            ivalue = _as_int(value, default=-1)
            if ivalue < 0:
                failures.append(f"summary: tcp_ip http3_stream_datagram_queue_pop_path_total[{key}] invalid={value}")
    http3_dispatch_path = obs_delta.get("http3_datagram_dispatch_path_total")
    if not isinstance(http3_dispatch_path, dict):
        failures.append("summary: tcp_ip http3_datagram_dispatch_path_total missing/not-object")
    else:
        for key in ("receive_ok", "parse_err", "invalid_quarter_stream_id", "stream_not_found", "enqueue_ok", "receive_err"):
            value = http3_dispatch_path.get(key, 0)
            ivalue = _as_int(value, default=-1)
            if ivalue < 0:
                failures.append(f"summary: tcp_ip http3_datagram_dispatch_path_total[{key}] invalid={value}")
    http3_receive_wait_path = obs_delta.get("http3_datagram_receive_wait_path_total")
    if not isinstance(http3_receive_wait_path, dict):
        failures.append("summary: tcp_ip http3_datagram_receive_wait_path_total missing/not-object")
    else:
        for key in ("wait_enter", "return_ok", "return_err", "return_closed"):
            value = http3_receive_wait_path.get(key, 0)
            ivalue = _as_int(value, default=-1)
            if ivalue < 0:
                failures.append(f"summary: tcp_ip http3_datagram_receive_wait_path_total[{key}] invalid={value}")
    quic_receive_wait_path = obs_delta.get("quic_datagram_receive_wait_path_total")
    if not isinstance(quic_receive_wait_path, dict):
        failures.append("summary: tcp_ip quic_datagram_receive_wait_path_total missing/not-object")
    else:
        for key in ("wait_enter", "return_ok", "return_err", "return_closed"):
            value = quic_receive_wait_path.get(key, 0)
            ivalue = _as_int(value, default=-1)
            if ivalue < 0:
                failures.append(f"summary: tcp_ip quic_datagram_receive_wait_path_total[{key}] invalid={value}")
    quic_packet_receive_drop_path = obs_delta.get("quic_packet_receive_drop_path_total")
    if not isinstance(quic_packet_receive_drop_path, dict):
        failures.append("summary: tcp_ip quic_packet_receive_drop_path_total missing/not-object")
    else:
        for key in ("conn_queue_full_drop", "server_queue_full_drop"):
            value = quic_packet_receive_drop_path.get(key, 0)
            ivalue = _as_int(value, default=-1)
            if ivalue < 0:
                failures.append(f"summary: tcp_ip quic_packet_receive_drop_path_total[{key}] invalid={value}")
    quic_packet_receive_ingress_path = obs_delta.get("quic_packet_receive_ingress_path_total")
    if not isinstance(quic_packet_receive_ingress_path, dict):
        failures.append("summary: tcp_ip quic_packet_receive_ingress_path_total missing/not-object")
    else:
        for key in (
            "transport_read_packet_total",
            "ingress_recv_empty_total",
            "ingress_demux_parse_conn_id_err_total",
            "ingress_demux_routed_to_conn_total",
            "ingress_demux_short_unknown_conn_drop_total",
            "ingress_demux_long_server_queue_total",
            "ingress_conn_ring_enqueue_total",
            "ingress_handlepackets_pop_total",
            "ingress_short_header_enter_total",
            "ingress_short_header_dest_cid_parse_err_total",
        ):
            value = quic_packet_receive_ingress_path.get(key, 0)
            ivalue = _as_int(value, default=-1)
            if ivalue < 0:
                failures.append(f"summary: tcp_ip quic_packet_receive_ingress_path_total[{key}] invalid={value}")
    quic_post_decrypt_path = obs_delta.get("quic_datagram_post_decrypt_path_total")
    if not isinstance(quic_post_decrypt_path, dict):
        failures.append("summary: tcp_ip quic_datagram_post_decrypt_path_total missing/not-object")
    else:
        for key in (
            "short_unpack_ok",
            "short_unpack_err",
            "payload_has_datagram_frame",
            "payload_without_datagram_frame",
            "payload_parse_err",
            "contains_datagram_frame",
            "ack_only_or_control_only",
            "contains_stream_without_datagram_frame",
        ):
            value = quic_post_decrypt_path.get(key, 0)
            ivalue = _as_int(value, default=-1)
            if ivalue < 0:
                failures.append(f"summary: tcp_ip quic_datagram_post_decrypt_path_total[{key}] invalid={value}")
    quic_pre_ingress_path = obs_delta.get("quic_datagram_pre_ingress_path_total")
    if not isinstance(quic_pre_ingress_path, dict):
        failures.append("summary: tcp_ip quic_datagram_pre_ingress_path_total missing/not-object")
    else:
        for key in ("packet_without_datagram_frame", "frame_type_seen", "parse_err", "skip_handling", "handle_call"):
            value = quic_pre_ingress_path.get(key, 0)
            ivalue = _as_int(value, default=-1)
            if ivalue < 0:
                failures.append(f"summary: tcp_ip quic_datagram_pre_ingress_path_total[{key}] invalid={value}")
    quic_send_path = obs_delta.get("quic_datagram_send_path_total")
    if not isinstance(quic_send_path, dict):
        failures.append("summary: tcp_ip quic_datagram_send_path_total missing/not-object")
    else:
        for key in (
            "contains_datagram_frame",
            "ack_only_or_control_only",
            "contains_stream_without_datagram_frame",
        ):
            value = quic_send_path.get(key, 0)
            ivalue = _as_int(value, default=-1)
            if ivalue < 0:
                failures.append(f"summary: tcp_ip quic_datagram_send_path_total[{key}] invalid={value}")
    quic_send_pipeline_path = obs_delta.get("quic_datagram_send_pipeline_path_total")
    if not isinstance(quic_send_pipeline_path, dict):
        failures.append("summary: tcp_ip quic_datagram_send_pipeline_path_total missing/not-object")
    else:
        for key in (
            "packed_with_datagram",
            "encrypted_with_datagram",
            "send_queue_enqueued",
        ):
            value = quic_send_pipeline_path.get(key, 0)
            ivalue = _as_int(value, default=-1)
            if ivalue < 0:
                failures.append(f"summary: tcp_ip quic_datagram_send_pipeline_path_total[{key}] invalid={value}")
    quic_send_write_path = obs_delta.get("quic_datagram_send_write_path_total")
    if not isinstance(quic_send_write_path, dict):
        failures.append("summary: tcp_ip quic_datagram_send_write_path_total missing/not-object")
    else:
        for key in (
            "send_loop_enter",
            "write_attempt",
            "write_ok",
            "write_err",
        ):
            value = quic_send_write_path.get(key, 0)
            ivalue = _as_int(value, default=-1)
            if ivalue < 0:
                failures.append(f"summary: tcp_ip quic_datagram_send_write_path_total[{key}] invalid={value}")
    quic_tx_path = obs_delta.get("quic_datagram_tx_path_total")
    if not isinstance(quic_tx_path, dict):
        failures.append("summary: tcp_ip quic_datagram_tx_path_total missing/not-object")
    else:
        for key in (
            "tx_path_enter",
            "sendmsg_attempt",
            "sendmsg_ok",
            "sendmsg_err",
        ):
            value = quic_tx_path.get(key, 0)
            ivalue = _as_int(value, default=-1)
            if ivalue < 0:
                failures.append(f"summary: tcp_ip quic_datagram_tx_path_total[{key}] invalid={value}")
    quic_tx_packet_len = obs_delta.get("quic_datagram_tx_packet_len_total")
    if not isinstance(quic_tx_packet_len, dict):
        failures.append("summary: tcp_ip quic_datagram_tx_packet_len_total missing/not-object")
    else:
        for key in (
            "le_256",
            "le_512",
            "le_1024",
            "le_1200",
            "le_1400",
            "gt_1400",
        ):
            value = quic_tx_packet_len.get(key, 0)
            ivalue = _as_int(value, default=-1)
            if ivalue < 0:
                failures.append(f"summary: tcp_ip quic_datagram_tx_packet_len_total[{key}] invalid={value}")
    quic_ingress_path = obs_delta.get("quic_datagram_ingress_path_total")
    if not isinstance(quic_ingress_path, dict):
        failures.append("summary: tcp_ip quic_datagram_ingress_path_total missing/not-object")
    else:
        for key in ("frame_enter", "frame_reject_too_large", "enqueue_ok"):
            value = quic_ingress_path.get(key, 0)
            ivalue = _as_int(value, default=-1)
            if ivalue < 0:
                failures.append(f"summary: tcp_ip quic_datagram_ingress_path_total[{key}] invalid={value}")
    quic_queue_pop_path = obs_delta.get("quic_datagram_rcv_queue_pop_path_total")
    if not isinstance(quic_queue_pop_path, dict):
        failures.append("summary: tcp_ip quic_datagram_rcv_queue_pop_path_total missing/not-object")
    else:
        for key in ("queue_pop_ok", "queue_empty", "queue_closed", "queue_pop_err"):
            value = quic_queue_pop_path.get(key, 0)
            ivalue = _as_int(value, default=-1)
            if ivalue < 0:
                failures.append(f"summary: tcp_ip quic_datagram_rcv_queue_pop_path_total[{key}] invalid={value}")
    stop_reason = str(tcp_ip.get("stop_reason", "")).strip().lower()
    tx_sendmsg_ok = _as_int((quic_tx_path or {}).get("sendmsg_ok", 0), 0)
    tx_le_1400 = _as_int((quic_tx_packet_len or {}).get("le_1400", 0), 0)
    send_queue_enqueued = _as_int((quic_send_pipeline_path or {}).get("send_queue_enqueued", 0), 0)
    send_write_ok = _as_int((quic_send_write_path or {}).get("write_ok", 0), 0)
    post_contains_datagram = _as_int((quic_post_decrypt_path or {}).get("contains_datagram_frame", 0), 0)
    post_payload_has_datagram = _as_int((quic_post_decrypt_path or {}).get("payload_has_datagram_frame", 0), 0)
    pre_frame_seen = _as_int((quic_pre_ingress_path or {}).get("frame_type_seen", 0), 0)
    frame_visibility_absent_branch = (
        tx_sendmsg_ok > 0
        and tx_le_1400 > 0
        and send_queue_enqueued > 0
        and send_write_ok > 0
        and post_contains_datagram == 0
        and pre_frame_seen == 0
    )
    enforce_post_send_stop_reason = not bool(tcp_ip.get("ok"))
    if (
        enforce_post_send_stop_reason
        and frame_visibility_absent_branch
        and stop_reason != "post_send_frame_visibility_absent"
    ):
        failures.append(
            "summary: tcp_ip frame-visibility branch requires "
            "stop_reason=post_send_frame_visibility_absent"
        )
    remote_visibility_absent = post_payload_has_datagram == 0 and pre_frame_seen == 0
    post_send_remote_visibility_absent_correlation = (
        tx_sendmsg_ok > 0
        and send_queue_enqueued > 0
        and send_write_ok > 0
        and remote_visibility_absent
    )
    if (
        enforce_post_send_stop_reason
        and post_send_remote_visibility_absent_correlation
        and stop_reason != "post_send_frame_visibility_absent"
    ):
        failures.append(
            "summary: tcp_ip post-send remote-visibility correlation requires "
            "stop_reason=post_send_frame_visibility_absent"
        )

    classified = _as_int(obs_delta.get("connect_ip_engine_classified_total", 0), 0)
    packet_tx = _as_int(obs_delta.get("connect_ip_packet_tx_total", 0), 0)
    write_fail = _as_int(obs_delta.get("connect_ip_packet_write_fail_total", 0), 0)
    read_exit = _as_int(obs_delta.get("connect_ip_packet_read_exit_total", 0), 0)
    if classified <= 0 and packet_tx <= 0:
        failures.append("summary: tcp_ip observability requires classified_total>0 or packet_tx_total>0")
    if write_fail != 0:
        failures.append(f"summary: tcp_ip packet_write_fail_total={write_fail} expected=0")
    if read_exit != 0:
        failures.append(f"summary: tcp_ip packet_read_exit_total={read_exit} expected=0")

    return {
        "summary_ok": True,
        "connect_ip_bridge_write_err_reason_parity": {
            "ok": len(unknown_reason_buckets) == 0,
            "allowed_reason_buckets": sorted(CONNECT_IP_BRIDGE_ALLOWED_REASON_BUCKETS),
            "unknown_reason_buckets": sorted(set(unknown_reason_buckets)),
        },
        "connect_ip_post_send_frame_visibility_branch": {
            "ok": (not frame_visibility_absent_branch)
            or (not enforce_post_send_stop_reason)
            or stop_reason == "post_send_frame_visibility_absent",
            "active": frame_visibility_absent_branch,
            "stop_reason": stop_reason,
            "tx_sendmsg_ok": tx_sendmsg_ok,
            "tx_packet_len_le_1400": tx_le_1400,
            "send_queue_enqueued": send_queue_enqueued,
            "send_write_ok": send_write_ok,
            "post_contains_datagram_frame": post_contains_datagram,
            "pre_frame_type_seen": pre_frame_seen,
        },
        "connect_ip_post_send_remote_visibility_correlation": {
            "ok": (not post_send_remote_visibility_absent_correlation)
            or (not enforce_post_send_stop_reason)
            or stop_reason == "post_send_frame_visibility_absent",
            "active": post_send_remote_visibility_absent_correlation,
            "stop_reason": stop_reason,
            "tx_sendmsg_ok": tx_sendmsg_ok,
            "send_queue_enqueued": send_queue_enqueued,
            "send_write_ok": send_write_ok,
            "remote_payload_has_datagram_frame": post_payload_has_datagram,
            "remote_pre_frame_type_seen": pre_frame_seen,
            "remote_visibility_absent": remote_visibility_absent,
        },
    }


def _check_runtime_harness_artifacts(runtime_dir: Path, failures: list):
    artifacts = {
        "peer_abort": {
            "file": "peer_abort_lifecycle_runtime.json",
            "actual": {"lifecycle"},
            "result": {"lifecycle"},
            "sources": {"runtime"},
        },
        "malformed_scoped": {
            "file": "malformed_scoped_runtime.json",
            "actual": {"capability", "policy"},
            "result": {"capability", "policy"},
            "sources": {"runtime"},
        },
        "malformed_scoped_transport": {
            "file": "malformed_scoped_transport_runtime.json",
            "actual": {"capability", "policy"},
            "result": {"capability", "policy"},
            "sources": {"runtime"},
        },
        "route_advertise": {
            "file": "route_advertise_dual_signal_runtime.json",
            "actual": {"capability"},
            "result": {"lifecycle"},
            "sources": {"runtime"},
        },
    }

    checks = {}
    for name, spec in artifacts.items():
        data = _load_json(runtime_dir / spec["file"], failures, f"artifact:{name}")
        if not isinstance(data, dict):
            checks[name] = {"ok": False}
            continue
        ok = bool(data.get("ok"))
        actual = str(data.get("actual_error_class", "")).strip().lower()
        result = str(data.get("result_error_class", "")).strip().lower()
        consistent = bool(data.get("error_class_consistent")) is True
        source = str(data.get("error_source", "")).strip().lower()
        if not ok:
            failures.append(f"artifact:{name}: ok=false")
        if actual not in spec["actual"]:
            failures.append(f"artifact:{name}: actual_error_class={actual} expected={sorted(spec['actual'])}")
        if result not in spec["result"]:
            failures.append(f"artifact:{name}: result_error_class={result} expected={sorted(spec['result'])}")
        if not consistent:
            failures.append(f"artifact:{name}: error_class_consistent={data.get('error_class_consistent')} expected=true")
        if source not in spec["sources"]:
            failures.append(f"artifact:{name}: error_source={source} expected={sorted(spec['sources'])}")
        checks[name] = {
            "ok": ok,
            "actual_error_class": actual,
            "result_error_class": result,
            "error_class_consistent": consistent,
            "error_source": source,
        }
    malformed_runtime = checks.get("malformed_scoped", {})
    malformed_transport = checks.get("malformed_scoped_transport", {})
    parity_ok = (
        bool(malformed_runtime.get("ok"))
        and bool(malformed_transport.get("ok"))
        and malformed_runtime.get("actual_error_class") == malformed_transport.get("actual_error_class")
        and malformed_runtime.get("result_error_class") == malformed_transport.get("result_error_class")
        and bool(malformed_runtime.get("error_class_consistent"))
        and bool(malformed_transport.get("error_class_consistent"))
    )
    if not parity_ok:
        failures.append(
            "artifact:malformed_scoped_boundary_parity: runtime/transport class mismatch or inconsistent classification"
        )
    checks["malformed_scoped_boundary_parity"] = {
        "ok": parity_ok,
        "runtime_actual_error_class": malformed_runtime.get("actual_error_class"),
        "runtime_result_error_class": malformed_runtime.get("result_error_class"),
        "transport_actual_error_class": malformed_transport.get("actual_error_class"),
        "transport_result_error_class": malformed_transport.get("result_error_class"),
    }
    return checks


def _check_runtime_artifacts_error_source_gate(runtime_artifacts: dict, failures: list):
    row_names = ("peer_abort", "route_advertise")
    rows = {}
    all_ok = True
    for name in row_names:
        row = runtime_artifacts.get(name)
        if not isinstance(row, dict):
            failures.append(f"runtime_artifacts_error_source:{name}: missing object")
            rows[name] = {"ok": False, "error_source": None}
            all_ok = False
            continue
        source = str(row.get("error_source", "")).strip().lower()
        row_ok = source == "runtime"
        if not row_ok:
            failures.append(
                f"runtime_artifacts_error_source:{name}: error_source={row.get('error_source')} expected=runtime"
            )
            all_ok = False
        rows[name] = {"ok": row_ok, "error_source": source}
    return {"ok": all_ok, "rows": rows}


def _check_runtime_single_source_drift(checks: dict, runtime_artifacts: dict, failures: list):
    if not isinstance(checks, dict):
        failures.append("runtime_single_source_drift: checks payload missing/invalid")
        return {"ok": False}

    forbidden_present = sorted(key for key in LEGACY_ADHOC_CHECK_KEYS if key in checks)
    if forbidden_present:
        failures.append(
            "runtime_single_source_drift: legacy/ad-hoc checks present outside runtime_artifacts*: "
            + ",".join(forbidden_present)
        )

    rows = {}
    rows_ok = True
    for row_name in SINGLE_SOURCE_REQUIRED_ARTIFACT_ROWS:
        row = runtime_artifacts.get(row_name)
        row_ok = isinstance(row, dict)
        if not row_ok:
            failures.append(f"runtime_single_source_drift: runtime_artifacts.{row_name} missing object")
            rows_ok = False
        rows[row_name] = {"ok": row_ok}

    ok = not forbidden_present and rows_ok
    return {"ok": ok, "forbidden_present": forbidden_present, "rows": rows}


def _check_scoped_artifact(runtime_dir: Path, failures: list):
    data = _load_json(runtime_dir / "scoped_connect_ip_latest.json", failures, "scoped")
    if not isinstance(data, dict):
        return {"ok": False}

    if str(data.get("mode", "")).strip() != "connect_ip_scoped":
        failures.append(f"scoped: mode={data.get('mode')} expected=connect_ip_scoped")
    if str(data.get("result", "false")).lower() != "true":
        failures.append(f"scoped: result={data.get('result')}")

    positive = data.get("positive") or {}
    if not bool(positive.get("ok")):
        failures.append("scoped: positive.ok=false")
    if not bool(positive.get("scope_observability_ok")):
        failures.append("scoped: positive.scope_observability_ok=false")
    if str(positive.get("scope_target", "")).strip() != "10.200.0.2/32":
        failures.append(f"scoped: positive.scope_target={positive.get('scope_target')} expected=10.200.0.2/32")
    if _as_int(positive.get("scope_ipproto", -1), -1) != 17:
        failures.append(f"scoped: positive.scope_ipproto={positive.get('scope_ipproto')} expected=17")

    negative_malformed = data.get("negative_malformed_target") or {}
    negative_actual = str(negative_malformed.get("actual_error_class", "")).strip().lower()
    negative_result = str(negative_malformed.get("result_error_class", "")).strip().lower()
    negative_consistent = bool(negative_malformed.get("error_class_consistent")) is True
    negative_source = str(negative_malformed.get("error_source", "")).strip().lower()
    if not bool(negative_malformed.get("ok")):
        failures.append("scoped: negative_malformed_target.ok=false")
    if negative_actual not in SCOPED_CLASS_ALLOWED:
        failures.append(
            f"scoped: negative_malformed_target.actual_error_class={negative_malformed.get('actual_error_class')} expected=capability|policy"
        )
    if negative_result not in SCOPED_CLASS_ALLOWED:
        failures.append(
            f"scoped: negative_malformed_target.result_error_class={negative_malformed.get('result_error_class')} expected=capability|policy"
        )
    if not negative_consistent:
        failures.append("scoped: negative_malformed_target.error_class_consistent expected=true")
    if negative_source not in SCOPED_SOURCE_ALLOWED:
        failures.append(
            f"scoped: negative_malformed_target.error_source={negative_malformed.get('error_source')} expected=runtime|compose_up"
        )

    negative_peer_abort = data.get("negative_peer_abort") or {}
    if not bool(negative_peer_abort.get("ok")):
        failures.append("scoped: negative_peer_abort.ok=false")
    if str(negative_peer_abort.get("actual_error_class", "")).strip().lower() != "lifecycle":
        failures.append(
            f"scoped: negative_peer_abort.actual_error_class={negative_peer_abort.get('actual_error_class')} expected=lifecycle"
        )
    if str(negative_peer_abort.get("result_error_class", "")).strip().lower() != "lifecycle":
        failures.append(
            f"scoped: negative_peer_abort.result_error_class={negative_peer_abort.get('result_error_class')} expected=lifecycle"
        )
    if bool(negative_peer_abort.get("error_class_consistent")) is not True:
        failures.append("scoped: negative_peer_abort.error_class_consistent expected=true")
    if str(negative_peer_abort.get("error_source", "")).strip().lower() not in SCOPED_SOURCE_ALLOWED:
        failures.append(
            f"scoped: negative_peer_abort.error_source={negative_peer_abort.get('error_source')} expected=runtime|compose_up"
        )

    negative_route = data.get("negative_peer_invalid_route_advertisement") or {}
    if not bool(negative_route.get("ok")):
        failures.append("scoped: negative_peer_invalid_route_advertisement.ok=false")
    if str(negative_route.get("actual_error_class", "")).strip().lower() != "capability":
        failures.append(
            "scoped: negative_peer_invalid_route_advertisement.actual_error_class="
            f"{negative_route.get('actual_error_class')} expected=capability"
        )
    if str(negative_route.get("result_error_class", "")).strip().lower() != "lifecycle":
        failures.append(
            "scoped: negative_peer_invalid_route_advertisement.result_error_class="
            f"{negative_route.get('result_error_class')} expected=lifecycle"
        )
    if bool(negative_route.get("error_class_consistent")) is not True:
        failures.append("scoped: negative_peer_invalid_route_advertisement.error_class_consistent expected=true")
    if str(negative_route.get("error_source", "")).strip().lower() not in SCOPED_SOURCE_ALLOWED:
        failures.append(
            "scoped: negative_peer_invalid_route_advertisement.error_source="
            f"{negative_route.get('error_source')} expected=runtime|compose_up"
        )

    malformed_harness = _load_json(runtime_dir / "malformed_scoped_runtime.json", failures, "artifact:malformed_scoped_for_scoped_contract")
    malformed_schema_ok = (
        bool(negative_malformed.get("ok"))
        and negative_actual in SCOPED_CLASS_ALLOWED
        and negative_result in SCOPED_CLASS_ALLOWED
        and negative_consistent
        and negative_source in SCOPED_SOURCE_ALLOWED
    )
    if not malformed_schema_ok:
        failures.append("scoped: negative_malformed_target schema contract failed")
    typed_parity_ok = False
    typed_source_parity_ok = False
    if isinstance(malformed_harness, dict):
        runtime_actual = str(malformed_harness.get("actual_error_class", "")).strip().lower()
        runtime_result = str(malformed_harness.get("result_error_class", "")).strip().lower()
        runtime_consistent = bool(malformed_harness.get("error_class_consistent")) is True
        runtime_source = str(malformed_harness.get("error_source", "")).strip().lower()
        typed_parity_ok = (
            runtime_actual == negative_actual
            and runtime_result == negative_result
            and runtime_consistent
            and negative_consistent
        )
        typed_source_parity_ok = runtime_source == "runtime" and negative_source in SCOPED_SOURCE_ALLOWED
        if not typed_parity_ok:
            failures.append(
                "scoped: negative_malformed_target typed parity mismatch against malformed_scoped_runtime artifact"
            )
        if not typed_source_parity_ok:
            failures.append(
                "scoped: negative_malformed_target source parity mismatch against malformed_scoped_runtime artifact"
            )
    return {
        "ok": True,
        "negative_malformed_target_schema_ok": malformed_schema_ok,
        "negative_malformed_target_typed_parity_ok": typed_parity_ok,
        "negative_malformed_target_typed_source_parity_ok": typed_source_parity_ok,
        "negative_malformed_target_actual_error_class": negative_actual,
        "negative_malformed_target_result_error_class": negative_result,
        "negative_malformed_target_error_source": negative_source,
    }


def _check_scoped_error_source_enum(runtime_dir: Path, failures: list):
    scoped = _load_json(runtime_dir / "scoped_connect_ip_latest.json", failures, "scoped:error_source_enum")
    if not isinstance(scoped, dict):
        return {"ok": False}

    row_names = (
        "negative_malformed_target",
        "negative_peer_abort",
        "negative_peer_invalid_route_advertisement",
    )
    rows = {}
    all_ok = True
    for name in row_names:
        row = scoped.get(name)
        if not isinstance(row, dict):
            failures.append(f"scoped:error_source_enum:{name}: missing object")
            rows[name] = {"ok": False, "error_source": None}
            all_ok = False
            continue
        source = str(row.get("error_source", "")).strip().lower()
        row_ok = source in SCOPED_SOURCE_ALLOWED
        if not row_ok:
            failures.append(
                f"scoped:error_source_enum:{name}: error_source={row.get('error_source')} expected=runtime|compose_up"
            )
            all_ok = False
        rows[name] = {"ok": row_ok, "error_source": source}

    return {"ok": all_ok, "allowed_values": sorted(SCOPED_SOURCE_ALLOWED), "rows": rows}


def _check_scoped_runtime_cross_artifact_parity(runtime_dir: Path, failures: list):
    scoped = _load_json(runtime_dir / "scoped_connect_ip_latest.json", failures, "scoped:cross_artifact")
    peer_abort = _load_json(runtime_dir / "peer_abort_lifecycle_runtime.json", failures, "artifact:peer_abort:cross_artifact")
    route_advertise = _load_json(
        runtime_dir / "route_advertise_dual_signal_runtime.json", failures, "artifact:route_advertise:cross_artifact"
    )
    if not isinstance(scoped, dict) or not isinstance(peer_abort, dict) or not isinstance(route_advertise, dict):
        return {"ok": False}

    checks = {}
    cross_specs = (
        (
            "negative_peer_abort",
            scoped.get("negative_peer_abort"),
            peer_abort,
        ),
        (
            "negative_peer_invalid_route_advertisement",
            scoped.get("negative_peer_invalid_route_advertisement"),
            route_advertise,
        ),
    )
    all_ok = True
    for name, scoped_row, runtime_row in cross_specs:
        if not isinstance(scoped_row, dict):
            failures.append(f"scoped_cross:{name}: missing object in scoped_connect_ip_latest.json")
            checks[name] = {"ok": False}
            all_ok = False
            continue
        scoped_actual = str(scoped_row.get("actual_error_class", "")).strip().lower()
        scoped_result = str(scoped_row.get("result_error_class", "")).strip().lower()
        scoped_source = str(scoped_row.get("error_source", "")).strip().lower()
        runtime_actual = str(runtime_row.get("actual_error_class", "")).strip().lower()
        runtime_result = str(runtime_row.get("result_error_class", "")).strip().lower()
        runtime_source = str(runtime_row.get("error_source", "")).strip().lower()

        class_parity = scoped_actual == runtime_actual and scoped_result == runtime_result
        source_parity = runtime_source == "runtime" and scoped_source in {"runtime", "compose_up"}
        parity_ok = class_parity and source_parity
        if not parity_ok:
            failures.append(
                f"scoped_cross:{name}: mismatch "
                f"scoped(actual={scoped_actual}, result={scoped_result}, source={scoped_source}) "
                f"runtime(actual={runtime_actual}, result={runtime_result}, source={runtime_source})"
            )
            all_ok = False
        checks[name] = {
            "ok": parity_ok,
            "scoped_actual_error_class": scoped_actual,
            "scoped_result_error_class": scoped_result,
            "scoped_error_source": scoped_source,
            "runtime_actual_error_class": runtime_actual,
            "runtime_result_error_class": runtime_result,
            "runtime_error_source": runtime_source,
        }

    checks["ok"] = all_ok
    checks["negative_peer_abort_strict_ok"] = bool((checks.get("negative_peer_abort") or {}).get("ok"))
    checks["negative_peer_invalid_route_advertisement_strict_ok"] = bool(
        (checks.get("negative_peer_invalid_route_advertisement") or {}).get("ok")
    )
    return checks


def _check_smoke_files(runtime_dir: Path, failures: list):
    files = {
        "smoke_10kb_latest.json": ("connect_udp", 10240, 5000),
        "smoke_tcp_connect_stream_latest.json": ("connect_stream", 10240, 5000),
        "smoke_tcp_connect_ip_latest.json": ("connect_ip", 10240, 5000),
    }
    checks = {}
    for name, (mode, min_bytes, max_ms) in files.items():
        data = _load_json(runtime_dir / name, failures, f"smoke:{name}")
        if not isinstance(data, dict):
            checks[name] = {"ok": False}
            continue
        if data.get("mode") != mode:
            failures.append(f"smoke:{name}: mode={data.get('mode')} expected={mode}")
        if str(data.get("result", "false")).lower() != "true":
            failures.append(f"smoke:{name}: result={data.get('result')}, error_class={data.get('error_class')}")
        metrics = data.get("metrics") or {}
        thresholds = data.get("thresholds") or {}
        if _as_int(thresholds.get("min_bytes", -1), -1) != min_bytes:
            failures.append(f"smoke:{name}: thresholds.min_bytes={thresholds.get('min_bytes')} expected={min_bytes}")
        if _as_int(thresholds.get("max_elapsed_ms", -1), -1) != max_ms:
            failures.append(
                f"smoke:{name}: thresholds.max_elapsed_ms={thresholds.get('max_elapsed_ms')} expected={max_ms}"
            )
        if _as_int(metrics.get("bytes_received", 0), 0) < min_bytes:
            failures.append(f"smoke:{name}: bytes_received={metrics.get('bytes_received')} expected>={min_bytes}")
        if _as_int(metrics.get("elapsed_ms", max_ms + 1), max_ms + 1) > max_ms:
            failures.append(f"smoke:{name}: elapsed_ms={metrics.get('elapsed_ms')} expected<={max_ms}")
        checks[name] = {
            "ok": True,
            "mode": data.get("mode"),
        }
    return checks


def _check_anti_bypass_contract(runtime_dir: Path, failures: list):
    artifact = _load_json(runtime_dir / "anti_bypass_latest.json", failures, "anti_bypass")
    summary = _load_json(runtime_dir / "masque_python_runner_summary.json", failures, "anti_bypass:summary")
    if not isinstance(artifact, dict):
        return {"ok": False}
    check_failures = []
    if str(artifact.get("schema", "")).strip() != ANTI_BYPASS_SCHEMA:
        check_failures.append(
            f"anti_bypass: schema={artifact.get('schema')!r} expected={ANTI_BYPASS_SCHEMA!r}"
        )
    if _as_int(artifact.get("schema_version", -1), -1) != ANTI_BYPASS_SCHEMA_VERSION:
        check_failures.append(
            "anti_bypass: "
            f"schema_version={artifact.get('schema_version')!r} expected={ANTI_BYPASS_SCHEMA_VERSION!r}"
        )
    if not isinstance(artifact.get("ok"), bool):
        check_failures.append("anti_bypass: ok must be bool")
    if not isinstance(artifact.get("failures"), list):
        check_failures.append("anti_bypass: failures must be array")
    mode_rows = artifact.get("modes")
    if not isinstance(mode_rows, list) or not mode_rows:
        check_failures.append("anti_bypass: modes missing/empty")
        failures.extend(check_failures)
        return {"ok": False, "failures": check_failures}

    mode_index = {}
    for row in mode_rows:
        if not isinstance(row, dict):
            check_failures.append("anti_bypass: mode row must be object")
            continue
        mode = str(row.get("mode") or "").strip().lower()
        if not mode:
            check_failures.append("anti_bypass: mode row has empty mode")
            continue
        if mode in mode_index:
            check_failures.append(f"anti_bypass: duplicate mode row={mode!r}")
            continue
        mode_index[mode] = row

    row_checks = {}
    for mode, scenario in ANTI_BYPASS_EXPECTED_MODES.items():
        row = mode_index.get(mode)
        if not isinstance(row, dict):
            check_failures.append(f"anti_bypass: missing mode row={mode!r}")
            row_checks[mode] = {"ok": False}
            continue
        row_scenario = str(row.get("scenario") or "").strip()
        row_ok = bool(row.get("ok"))
        summary_ok = str(row.get("summary_ok", "true")).strip().lower()
        error_class = str(row.get("error_class") or "").strip().lower()
        error_source = _normalize_anti_bypass_error_source(row.get("error_source"))
        runner_exit = _as_int(row.get("runner_exit_code"), 0)
        row_failures = row.get("failures")
        row_valid = True
        if row_scenario != scenario:
            check_failures.append(
                f"anti_bypass:{mode}: scenario={row_scenario!r} expected={scenario!r}"
            )
            row_valid = False
        if not row_ok:
            check_failures.append(f"anti_bypass:{mode}: ok=false")
            row_valid = False
        if summary_ok != "false":
            check_failures.append(f"anti_bypass:{mode}: summary_ok={row.get('summary_ok')!r} expected=false")
            row_valid = False
        if error_class in {"", "none", "unknown"}:
            check_failures.append(f"anti_bypass:{mode}: error_class not classified ({row.get('error_class')!r})")
            row_valid = False
        if error_source not in {"runtime", "compose_up"}:
            check_failures.append(f"anti_bypass:{mode}: error_source={row.get('error_source')!r} expected runtime|compose_up")
            row_valid = False
        if runner_exit == 0:
            check_failures.append(f"anti_bypass:{mode}: runner_exit_code=0 expected non-zero")
            row_valid = False
        if not isinstance(row_failures, list):
            check_failures.append(f"anti_bypass:{mode}: failures must be array")
            row_valid = False
        row_checks[mode] = {
            "ok": row_valid,
            "scenario": row_scenario,
            "error_class": error_class,
            "error_source": error_source,
            "summary_ok": summary_ok,
            "runner_exit_code": runner_exit,
        }

    parity_failures = []
    parity_rows = {}
    parity_ok = False
    if isinstance(summary, dict):
        summary_rows = summary.get("results")
        summary_by_scenario = {}
        if isinstance(summary_rows, list):
            for row in summary_rows:
                if not isinstance(row, dict):
                    continue
                scenario = str(row.get("scenario") or "").strip()
                if scenario:
                    summary_by_scenario[scenario] = row
        else:
            parity_failures.append("anti_bypass:summary results missing/not-array")

        for mode, scenario in ANTI_BYPASS_EXPECTED_MODES.items():
            row = row_checks.get(mode)
            summary_row = summary_by_scenario.get(scenario)
            if not isinstance(row, dict):
                parity_rows[mode] = {"ok": False}
                parity_failures.append(f"anti_bypass:parity missing anti_bypass row mode={mode!r}")
                continue
            if not isinstance(summary_row, dict):
                parity_rows[mode] = {"ok": False}
                parity_failures.append(f"anti_bypass:parity missing summary scenario={scenario!r}")
                continue
            anti_error_class = str(row.get("error_class") or "").strip().lower()
            summary_error_class = str(summary_row.get("error_class") or "").strip().lower()
            anti_error_source = _normalize_anti_bypass_error_source(row.get("error_source"))
            summary_error_source = _normalize_anti_bypass_error_source(summary_row.get("error_source"))
            row_parity_ok = anti_error_class == summary_error_class and anti_error_source == summary_error_source
            if not row_parity_ok:
                parity_failures.append(
                    "anti_bypass:parity "
                    f"mode={mode} mismatch anti_bypass(class={anti_error_class}, source={anti_error_source}) "
                    f"summary(class={summary_error_class}, source={summary_error_source})"
                )
            parity_rows[mode] = {
                "ok": row_parity_ok,
                "error_class": anti_error_class,
                "summary_error_class": summary_error_class,
                "error_source": anti_error_source,
                "summary_error_source": summary_error_source,
            }
        parity_ok = len(parity_failures) == 0
    else:
        parity_failures.append("anti_bypass:summary missing/invalid")

    check_failures.extend(parity_failures)
    if check_failures:
        failures.extend(check_failures)
    return {
        "ok": len(check_failures) == 0,
        "rows": row_checks,
        "parity_with_summary": {
            "ok": parity_ok,
            "rows": parity_rows,
            "failures": parity_failures,
        },
        "failures": check_failures,
    }


def validate_runtime_contract(runtime_dir: Path):
    failures = []
    smoke_summary = _check_smoke_summary(runtime_dir, failures)
    runtime_artifacts = _check_runtime_harness_artifacts(runtime_dir, failures)
    runtime_artifacts_error_source = _check_runtime_artifacts_error_source_gate(runtime_artifacts, failures)
    scoped = _check_scoped_artifact(runtime_dir, failures)
    scoped_error_source_enum = _check_scoped_error_source_enum(runtime_dir, failures)
    scoped_cross_parity = _check_scoped_runtime_cross_artifact_parity(runtime_dir, failures)
    smoke_files = _check_smoke_files(runtime_dir, failures)
    anti_bypass_contract = _check_anti_bypass_contract(runtime_dir, failures)
    checks = {
        "summary": smoke_summary,
        "runtime_artifacts": runtime_artifacts,
        "runtime_artifacts_error_source": runtime_artifacts_error_source,
        "anti_bypass_contract": anti_bypass_contract,
        "scoped_contract": scoped,
        "scoped_error_source_enum": scoped_error_source_enum,
        "scoped_cross_artifact_parity": scoped_cross_parity,
        "smoke_files": smoke_files,
    }
    runtime_single_source_drift = _check_runtime_single_source_drift(checks, runtime_artifacts, failures)
    checks["runtime_single_source_drift"] = runtime_single_source_drift
    return {
        "schema": CONTRACT_SCHEMA,
        "schema_version": CONTRACT_SCHEMA_VERSION,
        "ok": len(failures) == 0,
        "runtime_dir": str(runtime_dir),
        "checks": checks,
        "failures": failures,
    }


def main():
    parser = argparse.ArgumentParser(description="Validate MASQUE runtime artifact contract")
    parser.add_argument("--runtime-dir", type=Path, default=RUNTIME_DIR_DEFAULT)
    parser.add_argument(
        "--output",
        type=Path,
        default=None,
        help="Optional output file for aggregated contract JSON (default: <runtime-dir>/masque_runtime_contract_latest.json)",
    )
    args = parser.parse_args()

    runtime_dir = args.runtime_dir.resolve()
    output = args.output or (runtime_dir / "masque_runtime_contract_latest.json")
    prewrite_failures = []
    _check_output_schema_compatibility(output, prewrite_failures)
    payload = validate_runtime_contract(runtime_dir)
    payload["failures"].extend(prewrite_failures)
    _check_payload_schema(payload, payload["failures"])
    payload["ok"] = len(payload["failures"]) == 0
    output.parent.mkdir(parents=True, exist_ok=True)
    output.write_text(json.dumps(payload, indent=2), encoding="utf-8")

    if payload["ok"]:
        print(f"MASQUE runtime contract passed. Artifact: {output}")
        return 0

    print("MASQUE runtime contract failures:")
    for failure in payload["failures"]:
        print(" -", failure)
    print(f"Aggregated contract artifact: {output}")
    return 1


if __name__ == "__main__":
    sys.exit(main())
