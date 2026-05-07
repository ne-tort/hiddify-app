import json
import tempfile
import unittest
from pathlib import Path

from masque_runtime_contract_validator import (
    CONNECT_IP_BRIDGE_REQUIRED_DELTA_KEYS,
    _check_anti_bypass_contract,
    _check_smoke_summary,
    validate_runtime_contract,
)


def _base_artifact() -> dict:
    return {
        "schema": "masque_anti_bypass_contract",
        "schema_version": 1,
        "ok": True,
        "modes": [
            {
                "mode": "tcp_stream",
                "scenario": "tcp_stream",
                "ok": True,
                "summary_ok": "false",
                "runner_exit_code": 1,
                "error_class": "transport_init",
                "error_source": "runtime",
                "failures": [],
            },
            {
                "mode": "udp",
                "scenario": "udp",
                "ok": True,
                "summary_ok": "false",
                "runner_exit_code": 1,
                "error_class": "transport_init",
                "error_source": "runtime",
                "failures": [],
            },
            {
                "mode": "tcp_ip",
                "scenario": "tcp_ip",
                "ok": True,
                "summary_ok": "false",
                "runner_exit_code": 1,
                "error_class": "transport_init",
                "error_source": "runtime",
                "failures": [],
            },
        ],
        "failures": [],
    }


def _base_summary() -> dict:
    return {
        "ok": False,
        "results": [
            {"scenario": "tcp_stream", "ok": False, "error_class": "transport_init", "error_source": "runtime"},
            {"scenario": "udp", "ok": False, "error_class": "transport_init", "error_source": "runtime"},
            {"scenario": "tcp_ip", "ok": False, "error_class": "transport_init", "error_source": "runtime"},
        ],
    }


class TestAntiBypassParityWithSummary(unittest.TestCase):
    def _run_check(self, artifact: dict, summary: dict):
        with tempfile.TemporaryDirectory() as temp_dir:
            runtime_dir = Path(temp_dir)
            (runtime_dir / "anti_bypass_latest.json").write_text(json.dumps(artifact), encoding="utf-8")
            (runtime_dir / "masque_python_runner_summary.json").write_text(json.dumps(summary), encoding="utf-8")
            failures: list[str] = []
            result = _check_anti_bypass_contract(runtime_dir, failures)
            return result, failures

    def test_parity_fails_when_summary_scenario_row_missing(self):
        artifact = _base_artifact()
        summary = _base_summary()
        summary["results"] = [row for row in summary["results"] if row["scenario"] != "tcp_ip"]

        result, failures = self._run_check(artifact, summary)

        self.assertFalse(result["parity_with_summary"]["ok"])
        self.assertFalse(result["parity_with_summary"]["rows"]["tcp_ip"]["ok"])
        self.assertIn("anti_bypass:parity missing summary scenario='tcp_ip'", failures)

    def test_parity_fails_when_error_class_mismatch(self):
        artifact = _base_artifact()
        summary = _base_summary()
        for row in summary["results"]:
            if row["scenario"] == "udp":
                row["error_class"] = "policy"

        result, failures = self._run_check(artifact, summary)

        self.assertFalse(result["parity_with_summary"]["ok"])
        self.assertFalse(result["parity_with_summary"]["rows"]["udp"]["ok"])
        self.assertTrue(
            any("anti_bypass:parity mode=udp mismatch anti_bypass" in failure for failure in failures),
            msg=f"expected udp parity mismatch failure, got: {failures}",
        )

    def test_parity_fails_when_error_source_mismatch(self):
        artifact = _base_artifact()
        summary = _base_summary()
        for row in artifact["modes"]:
            if row["mode"] == "tcp_stream":
                row["error_source"] = "compose_up"

        result, failures = self._run_check(artifact, summary)

        self.assertFalse(result["parity_with_summary"]["ok"])
        self.assertFalse(result["parity_with_summary"]["rows"]["tcp_stream"]["ok"])
        self.assertTrue(
            any("anti_bypass:parity mode=tcp_stream mismatch anti_bypass" in failure for failure in failures),
            msg=f"expected tcp_stream parity mismatch failure, got: {failures}",
        )

    def test_contract_fails_when_mode_row_missing(self):
        artifact = _base_artifact()
        artifact["modes"] = [row for row in artifact["modes"] if row["mode"] != "udp"]
        summary = _base_summary()

        result, failures = self._run_check(artifact, summary)

        self.assertFalse(result["ok"])
        self.assertFalse(result["rows"]["udp"]["ok"])
        self.assertFalse(result["parity_with_summary"]["rows"]["udp"]["ok"])
        self.assertIn("anti_bypass: missing mode row='udp'", failures)
        self.assertTrue(
            any("anti_bypass:parity mode=udp mismatch anti_bypass" in failure for failure in failures),
            msg=f"expected udp parity mismatch failure, got: {failures}",
        )

    def test_parity_passes_when_all_rows_match(self):
        artifact = _base_artifact()
        summary = _base_summary()

        result, failures = self._run_check(artifact, summary)

        self.assertTrue(result["ok"], msg=f"unexpected failures: {failures}")
        self.assertTrue(result["parity_with_summary"]["ok"], msg=f"unexpected failures: {failures}")
        for mode in ("tcp_stream", "udp", "tcp_ip"):
            self.assertTrue(result["rows"][mode]["ok"], msg=f"mode {mode} expected ok, failures={failures}")
            self.assertTrue(
                result["parity_with_summary"]["rows"][mode]["ok"],
                msg=f"mode {mode} parity expected ok, failures={failures}",
            )

    def test_parity_normalizes_unknown_or_empty_error_source_to_runtime(self):
        artifact = _base_artifact()
        summary = _base_summary()
        for row in artifact["modes"]:
            if row["mode"] == "udp":
                row["error_source"] = ""
        for row in summary["results"]:
            if row["scenario"] == "udp":
                row["error_source"] = "unexpected_source"

        result, failures = self._run_check(artifact, summary)

        self.assertTrue(result["ok"], msg=f"unexpected failures: {failures}")
        self.assertTrue(result["parity_with_summary"]["ok"], msg=f"unexpected failures: {failures}")
        self.assertEqual(result["rows"]["udp"]["error_source"], "runtime")
        self.assertEqual(result["parity_with_summary"]["rows"]["udp"]["error_source"], "runtime")
        self.assertEqual(result["parity_with_summary"]["rows"]["udp"]["summary_error_source"], "runtime")


class TestValidateRuntimeContractAntiBypassAggregation(unittest.TestCase):
    @staticmethod
    def _write_runtime_fixture(runtime_dir: Path, artifact: dict, summary: dict):
        (runtime_dir / "anti_bypass_latest.json").write_text(json.dumps(artifact), encoding="utf-8")
        (runtime_dir / "masque_python_runner_summary.json").write_text(json.dumps(summary), encoding="utf-8")

    def test_validate_runtime_contract_exports_anti_bypass_aggregates_green(self):
        artifact = _base_artifact()
        summary = _base_summary()

        with tempfile.TemporaryDirectory() as temp_dir:
            runtime_dir = Path(temp_dir)
            self._write_runtime_fixture(runtime_dir, artifact, summary)
            payload = validate_runtime_contract(runtime_dir)

        anti = payload["checks"]["anti_bypass_contract"]
        self.assertTrue(anti["ok"])
        self.assertTrue(anti["parity_with_summary"]["ok"])
        for mode in ("tcp_stream", "udp", "tcp_ip"):
            self.assertTrue(anti["rows"][mode]["ok"], msg=f"expected {mode}.ok=true")
            self.assertTrue(anti["parity_with_summary"]["rows"][mode]["ok"], msg=f"expected {mode} parity ok=true")

    def test_validate_runtime_contract_exports_anti_bypass_aggregates_red(self):
        artifact = _base_artifact()
        summary = _base_summary()
        for row in summary["results"]:
            if row["scenario"] == "udp":
                row["error_class"] = "policy"

        with tempfile.TemporaryDirectory() as temp_dir:
            runtime_dir = Path(temp_dir)
            self._write_runtime_fixture(runtime_dir, artifact, summary)
            payload = validate_runtime_contract(runtime_dir)

        anti = payload["checks"]["anti_bypass_contract"]
        self.assertFalse(anti["ok"])
        self.assertFalse(anti["parity_with_summary"]["ok"])
        self.assertFalse(anti["parity_with_summary"]["rows"]["udp"]["ok"])
        self.assertTrue(
            any("anti_bypass:parity mode=udp mismatch anti_bypass" in failure for failure in anti["failures"]),
            msg=f"expected udp parity mismatch failure, got: {anti['failures']}",
        )


def _base_tcp_ip_observability_delta() -> dict:
    delta = {key: 0 for key in CONNECT_IP_BRIDGE_REQUIRED_DELTA_KEYS}
    delta["connect_ip_bridge_write_err_reason_total"] = {}
    delta["connect_ip_policy_drop_icmp_reason_total"] = {}
    delta["connect_ip_proxied_packet_drop_reason_total"] = {}
    delta["connect_ip_receive_datagram_return_path_total"] = {}
    delta["connect_ip_receive_datagram_post_return_path_total"] = {}
    delta["connect_ip_bridge_readpacket_return_path_total"] = {}
    delta["connect_ip_engine_pmtu_update_reason_total"] = {}
    delta["http3_stream_datagram_queue_pop_path_total"] = {}
    delta["http3_datagram_dispatch_path_total"] = {}
    delta["http3_datagram_receive_wait_path_total"] = {}
    delta["quic_datagram_receive_wait_path_total"] = {}
    delta["quic_packet_receive_drop_path_total"] = {
        "conn_queue_full_drop": 0,
        "server_queue_full_drop": 0,
    }
    delta["quic_packet_receive_ingress_path_total"] = {
        "transport_read_packet_total": 0,
        "ingress_recv_empty_total": 0,
        "ingress_demux_parse_conn_id_err_total": 0,
        "ingress_demux_routed_to_conn_total": 0,
        "ingress_demux_short_unknown_conn_drop_total": 0,
        "ingress_demux_long_server_queue_total": 0,
        "ingress_conn_ring_enqueue_total": 0,
        "ingress_handlepackets_pop_total": 0,
        "ingress_short_header_enter_total": 0,
        "ingress_short_header_dest_cid_parse_err_total": 0,
    }
    delta["quic_datagram_post_decrypt_path_total"] = {
        "payload_has_datagram_frame": 0,
        "contains_datagram_frame": 0,
    }
    delta["quic_datagram_send_path_total"] = {}
    delta["quic_datagram_send_pipeline_path_total"] = {"send_queue_enqueued": 1}
    delta["quic_datagram_send_write_path_total"] = {"write_ok": 1}
    delta["quic_datagram_tx_path_total"] = {"sendmsg_ok": 1}
    delta["quic_datagram_tx_packet_len_total"] = {"le_1400": 1}
    delta["quic_datagram_pre_ingress_path_total"] = {"frame_type_seen": 0}
    delta["quic_datagram_ingress_path_total"] = {}
    delta["quic_datagram_rcv_queue_pop_path_total"] = {}
    delta["connect_ip_packet_tx_total"] = 1
    return delta


class TestConnectIPPostSendRemoteVisibilityCorrelation(unittest.TestCase):
    def _run_summary_check(self, stop_reason: str):
        summary = {
            "ok": True,
            "results": [
                {
                    "scenario": "tcp_ip",
                    "stop_reason": stop_reason,
                    "connect_ip_udp_bridge_contract": "ipv4_only",
                    "connect_ip_udp_bridge_ipv6_supported": False,
                    "observability": {"delta": _base_tcp_ip_observability_delta()},
                }
            ],
        }
        with tempfile.TemporaryDirectory() as temp_dir:
            runtime_dir = Path(temp_dir)
            (runtime_dir / "masque_python_runner_summary.json").write_text(json.dumps(summary), encoding="utf-8")
            failures: list[str] = []
            checks = _check_smoke_summary(runtime_dir, failures)
            return checks, failures

    def test_green_when_remote_visibility_absent_has_typed_stop_reason(self):
        checks, failures = self._run_summary_check("post_send_frame_visibility_absent")
        row = checks["connect_ip_post_send_remote_visibility_correlation"]
        self.assertTrue(row["active"])
        self.assertTrue(row["ok"], msg=f"unexpected failures: {failures}")
        self.assertEqual(row["remote_payload_has_datagram_frame"], 0)
        self.assertEqual(row["remote_pre_frame_type_seen"], 0)

    def test_red_when_remote_visibility_absent_has_mismatched_stop_reason(self):
        checks, failures = self._run_summary_check("receiver_incomplete")
        row = checks["connect_ip_post_send_remote_visibility_correlation"]
        self.assertTrue(row["active"])
        self.assertFalse(row["ok"])
        self.assertIn(
            "summary: tcp_ip post-send remote-visibility correlation requires stop_reason=post_send_frame_visibility_absent",
            failures,
        )


if __name__ == "__main__":
    unittest.main()
