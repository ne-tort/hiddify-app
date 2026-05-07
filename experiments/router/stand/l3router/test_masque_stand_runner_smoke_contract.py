import json
import os
import tempfile
import unittest
from pathlib import Path
from unittest import mock

import masque_stand_runner as runner


class TestSmokeContractArtifacts(unittest.TestCase):
    def test_smoke_contract_writes_error_fields(self):
        with tempfile.TemporaryDirectory() as temp_dir:
            runtime_dir = Path(temp_dir)
            original_runtime_dir = runner.RUNTIME_DIR
            try:
                runner.RUNTIME_DIR = runtime_dir
                runner._write_masque_smoke_contract_files(
                    [
                        {
                            "scenario": "tcp_ip",
                            "ok": False,
                            "bytes_received": 0,
                            "elapsed_sec": 0.0,
                            "error_class": "transport_init",
                            "error_source": "compose_up",
                        }
                    ],
                    runner.BYTES_10KB,
                )
            finally:
                runner.RUNTIME_DIR = original_runtime_dir

            payload = json.loads((runtime_dir / "smoke_tcp_connect_ip_latest.json").read_text(encoding="utf-8"))
            self.assertEqual(payload["result"], "false")
            self.assertEqual(payload["error_class"], "transport_init")
            self.assertEqual(payload["error_source"], "compose_up")

    def test_classify_runner_exception_source_detects_compose_up(self):
        original_skip = runner.skip_stand_compose_up
        try:
            runner.skip_stand_compose_up = lambda: False
            source = runner._classify_runner_exception_source(
                "tcp_ip",
                "docker compose up failed: container not ready: masque-server-core",
            )
        finally:
            runner.skip_stand_compose_up = original_skip
        self.assertEqual(source, "compose_up")

    def test_classify_runner_exception_source_keeps_runtime_for_non_compose(self):
        original_skip = runner.skip_stand_compose_up
        try:
            runner.skip_stand_compose_up = lambda: False
            source = runner._classify_runner_exception_source(
                "tcp_ip",
                "hash mismatch after transfer",
            )
        finally:
            runner.skip_stand_compose_up = original_skip
        self.assertEqual(source, "runtime")

    def test_normalize_observability_snapshot_post_decrypt_mandatory_keys_default_zero(self):
        snapshot = runner._normalize_observability_snapshot(
            {
                "quic_datagram_post_decrypt_path_total": {
                    "short_unpack_ok": 5,
                }
            }
        )
        post_decrypt = snapshot["quic_datagram_post_decrypt_path_total"]
        self.assertEqual(post_decrypt["short_unpack_ok"], 5)
        self.assertEqual(post_decrypt["contains_datagram_frame"], 0)
        self.assertEqual(post_decrypt["ack_only_or_control_only"], 0)
        self.assertEqual(post_decrypt["contains_stream_without_datagram_frame"], 0)

    def test_diff_observability_post_decrypt_preserves_mandatory_zero_buckets(self):
        before = runner._zero_observability_snapshot()
        after = runner._zero_observability_snapshot()
        after["quic_datagram_post_decrypt_path_total"] = {
            "payload_has_datagram_frame": 2,
        }
        delta = runner._diff_observability(before, after)
        post_decrypt = delta["quic_datagram_post_decrypt_path_total"]
        self.assertEqual(post_decrypt["payload_has_datagram_frame"], 2)
        self.assertEqual(post_decrypt["contains_datagram_frame"], 0)
        self.assertEqual(post_decrypt["ack_only_or_control_only"], 0)
        self.assertEqual(post_decrypt["contains_stream_without_datagram_frame"], 0)

    def test_merge_observability_delta_maxes_per_container_deltas(self):
        """Regression: diff(merge(abs)) can drop the peer when max baseline is one-sided."""
        da = runner._diff_observability(
            runner._zero_observability_snapshot(),
            runner._normalize_observability_snapshot(
                {"quic_datagram_post_decrypt_path_total": {"contains_datagram_frame": 2}}
            ),
        )
        db = runner._diff_observability(
            runner._zero_observability_snapshot(),
            runner._normalize_observability_snapshot(
                {"quic_datagram_post_decrypt_path_total": {"contains_datagram_frame": 9000}}
            ),
        )
        merged = runner._merge_observability_delta(da, db)
        self.assertEqual(
            merged["quic_datagram_post_decrypt_path_total"]["contains_datagram_frame"],
            9000,
        )

    def test_normalize_observability_snapshot_send_mandatory_keys_default_zero(self):
        snapshot = runner._normalize_observability_snapshot(
            {
                "quic_datagram_send_path_total": {
                    "contains_datagram_frame": 3,
                }
            }
        )
        send_path = snapshot["quic_datagram_send_path_total"]
        self.assertEqual(send_path["contains_datagram_frame"], 3)
        self.assertEqual(send_path["ack_only_or_control_only"], 0)
        self.assertEqual(send_path["contains_stream_without_datagram_frame"], 0)

    def test_diff_observability_send_preserves_mandatory_zero_buckets(self):
        before = runner._zero_observability_snapshot()
        after = runner._zero_observability_snapshot()
        after["quic_datagram_send_path_total"] = {
            "ack_only_or_control_only": 4,
        }
        delta = runner._diff_observability(before, after)
        send_path = delta["quic_datagram_send_path_total"]
        self.assertEqual(send_path["ack_only_or_control_only"], 4)
        self.assertEqual(send_path["contains_datagram_frame"], 0)
        self.assertEqual(send_path["contains_stream_without_datagram_frame"], 0)

    def test_normalize_observability_snapshot_send_pipeline_mandatory_keys_default_zero(self):
        snapshot = runner._normalize_observability_snapshot(
            {
                "quic_datagram_send_pipeline_path_total": {
                    "packed_with_datagram": 7,
                }
            }
        )
        send_pipeline_path = snapshot["quic_datagram_send_pipeline_path_total"]
        self.assertEqual(send_pipeline_path["packed_with_datagram"], 7)
        self.assertEqual(send_pipeline_path["encrypted_with_datagram"], 0)
        self.assertEqual(send_pipeline_path["send_queue_enqueued"], 0)

    def test_diff_observability_send_pipeline_preserves_mandatory_zero_buckets(self):
        before = runner._zero_observability_snapshot()
        after = runner._zero_observability_snapshot()
        after["quic_datagram_send_pipeline_path_total"] = {
            "send_queue_enqueued": 2,
        }
        delta = runner._diff_observability(before, after)
        send_pipeline_path = delta["quic_datagram_send_pipeline_path_total"]
        self.assertEqual(send_pipeline_path["send_queue_enqueued"], 2)
        self.assertEqual(send_pipeline_path["packed_with_datagram"], 0)
        self.assertEqual(send_pipeline_path["encrypted_with_datagram"], 0)

    def test_normalize_observability_snapshot_send_write_mandatory_keys_default_zero(self):
        snapshot = runner._normalize_observability_snapshot(
            {
                "quic_datagram_send_write_path_total": {
                    "write_ok": 5,
                }
            }
        )
        send_write_path = snapshot["quic_datagram_send_write_path_total"]
        self.assertEqual(send_write_path["write_ok"], 5)
        self.assertEqual(send_write_path["send_loop_enter"], 0)
        self.assertEqual(send_write_path["write_attempt"], 0)
        self.assertEqual(send_write_path["write_err"], 0)

    def test_diff_observability_send_write_preserves_mandatory_zero_buckets(self):
        before = runner._zero_observability_snapshot()
        after = runner._zero_observability_snapshot()
        after["quic_datagram_send_write_path_total"] = {
            "write_attempt": 3,
        }
        delta = runner._diff_observability(before, after)
        send_write_path = delta["quic_datagram_send_write_path_total"]
        self.assertEqual(send_write_path["write_attempt"], 3)
        self.assertEqual(send_write_path["send_loop_enter"], 0)
        self.assertEqual(send_write_path["write_ok"], 0)
        self.assertEqual(send_write_path["write_err"], 0)

    def test_normalize_observability_snapshot_tx_mandatory_keys_default_zero(self):
        snapshot = runner._normalize_observability_snapshot(
            {
                "quic_datagram_tx_path_total": {
                    "sendmsg_ok": 9,
                }
            }
        )
        tx_path = snapshot["quic_datagram_tx_path_total"]
        self.assertEqual(tx_path["sendmsg_ok"], 9)
        self.assertEqual(tx_path["tx_path_enter"], 0)
        self.assertEqual(tx_path["sendmsg_attempt"], 0)
        self.assertEqual(tx_path["sendmsg_err"], 0)

    def test_diff_observability_tx_preserves_mandatory_zero_buckets(self):
        before = runner._zero_observability_snapshot()
        after = runner._zero_observability_snapshot()
        after["quic_datagram_tx_path_total"] = {
            "sendmsg_attempt": 4,
        }
        delta = runner._diff_observability(before, after)
        tx_path = delta["quic_datagram_tx_path_total"]
        self.assertEqual(tx_path["sendmsg_attempt"], 4)
        self.assertEqual(tx_path["tx_path_enter"], 0)
        self.assertEqual(tx_path["sendmsg_ok"], 0)
        self.assertEqual(tx_path["sendmsg_err"], 0)

    def test_normalize_observability_snapshot_tx_packet_len_mandatory_keys_default_zero(self):
        snapshot = runner._normalize_observability_snapshot(
            {
                "quic_datagram_tx_packet_len_total": {
                    "le_1200": 7,
                }
            }
        )
        tx_packet_len = snapshot["quic_datagram_tx_packet_len_total"]
        self.assertEqual(tx_packet_len["le_1200"], 7)
        self.assertEqual(tx_packet_len["le_256"], 0)
        self.assertEqual(tx_packet_len["le_512"], 0)
        self.assertEqual(tx_packet_len["le_1024"], 0)
        self.assertEqual(tx_packet_len["le_1400"], 0)
        self.assertEqual(tx_packet_len["gt_1400"], 0)

    def test_diff_observability_tx_packet_len_preserves_mandatory_zero_buckets(self):
        before = runner._zero_observability_snapshot()
        after = runner._zero_observability_snapshot()
        after["quic_datagram_tx_packet_len_total"] = {
            "gt_1400": 3,
        }
        delta = runner._diff_observability(before, after)
        tx_packet_len = delta["quic_datagram_tx_packet_len_total"]
        self.assertEqual(tx_packet_len["gt_1400"], 3)
        self.assertEqual(tx_packet_len["le_256"], 0)
        self.assertEqual(tx_packet_len["le_512"], 0)
        self.assertEqual(tx_packet_len["le_1024"], 0)
        self.assertEqual(tx_packet_len["le_1200"], 0)
        self.assertEqual(tx_packet_len["le_1400"], 0)

    def test_classify_tcp_ip_stop_reason_prefers_post_send_frame_visibility_absent(self):
        obs = {
            "source": "runtime_snapshot_log_marker",
            "delta": {
                "connect_ip_bridge_udp_tx_attempt_total": 5,
                "connect_ip_packet_tx_total": 5,
                "connect_ip_packet_rx_total": 0,
                "connect_ip_engine_ingress_total": 0,
                "quic_datagram_tx_path_total": {"sendmsg_ok": 5},
                "quic_datagram_tx_packet_len_total": {"le_1400": 5},
                "quic_datagram_send_pipeline_path_total": {"send_queue_enqueued": 5},
                "quic_datagram_send_write_path_total": {"write_ok": 5},
                "quic_datagram_post_decrypt_path_total": {"contains_datagram_frame": 0},
                "quic_datagram_pre_ingress_path_total": {"frame_type_seen": 0},
            },
        }
        reason = runner.classify_tcp_ip_stop_reason(
            send_err=None,
            got=1024,
            expected=4096,
            hash_ok=False,
            settled=True,
            budget_exceeded=False,
            obs=obs,
        )
        self.assertEqual(reason, "post_send_frame_visibility_absent")

    def test_classify_tcp_ip_stop_reason_falls_back_to_bridge_boundary_stall(self):
        obs = {
            "source": "runtime_snapshot_log_marker",
            "delta": {
                "connect_ip_bridge_udp_tx_attempt_total": 5,
                "connect_ip_packet_tx_total": 5,
                "connect_ip_packet_rx_total": 0,
                "connect_ip_engine_ingress_total": 0,
            },
        }
        reason = runner.classify_tcp_ip_stop_reason(
            send_err=None,
            got=1024,
            expected=4096,
            hash_ok=False,
            settled=True,
            budget_exceeded=False,
            obs=obs,
        )
        self.assertEqual(reason, "bridge_boundary_stall")

    def test_parse_csv_tcp_ip_rates_default_and_trim(self):
        self.assertEqual(runner._parse_csv_tcp_ip_rates_csv(""), ["8m", "10m", "12m", "14m"])
        self.assertEqual(runner._parse_csv_tcp_ip_rates_csv("15m, 16m "), ["15m", "16m"])

    def test_parse_csv_udp_bps_default_and_trim(self):
        self.assertEqual(runner._parse_csv_udp_bps_rates(""), [30_000_000, 50_000_000, 70_000_000])
        self.assertEqual(runner._parse_csv_udp_bps_rates("100, 200"), [100, 200])


class TestStandBulkHarness(unittest.TestCase):
    _BC_10MIB = 10 * 1024 * 1024

    # Runner tuning vars that must not leak from the parent process into harness math tests.
    _BULK_HARNESS_ENV_RESET_KEYS = (
        "MASQUE_STAND_SLOW_DOCKER",
        "MASQUE_STAND_MIN_GOODPUT_MBPS",
        "MASQUE_STAND_MIN_GOODPUT_MAX_BYTES",
        "MASQUE_STAND_MIN_GOODPUT_WALL_CAP_SEC",
        "MASQUE_BULK_STALL_FLOOR_SEC",
        "MASQUE_BULK_STALL_MULT",
        "MASQUE_STAND_TCP_IP_MIN_STRICT_SEC",
        "MASQUE_STAND_TCP_IP_STRICT_SEC_PER_MIB",
    )

    def _bulk_harness_env(self, **extra: str) -> dict[str, str]:
        env = {k: v for k, v in os.environ.items()}
        for key in self._BULK_HARNESS_ENV_RESET_KEYS:
            env.pop(key, None)
        env.update(extra)
        return env

    def test_udp_bulk_wall_10mib_default_matches_legacy(self):
        """Without MASQUE_STAND_* overrides, 10 MiB stays on 90s floor (CI-neutral)."""
        with mock.patch.dict(
            os.environ, self._bulk_harness_env(), clear=True
        ):
            w = runner._udp_tcp_stream_bulk_stall_wall_sec(self._BC_10MIB)
        self.assertEqual(w, 90)

    def test_udp_bulk_wall_slow_profile_raises_min_goodput(self):
        with mock.patch.dict(
            os.environ,
            self._bulk_harness_env(MASQUE_STAND_SLOW_DOCKER="1"),
            clear=True,
        ):
            w = runner._udp_tcp_stream_bulk_stall_wall_sec(self._BC_10MIB)
        self.assertGreaterEqual(w, 70)
        self.assertLessEqual(w, 120)

    def test_tcp_ip_min_strict_absolute_overrides(self):
        with mock.patch.dict(
            os.environ,
            self._bulk_harness_env(MASQUE_STAND_TCP_IP_MIN_STRICT_SEC="99"),
            clear=True,
        ):
            self.assertEqual(runner._tcp_ip_bulk_min_strict_budget_sec(self._BC_10MIB), 99)

    def test_tcp_ip_min_strict_slow_uses_sec_per_mib(self):
        with mock.patch.dict(
            os.environ,
            self._bulk_harness_env(MASQUE_STAND_SLOW_DOCKER="1"),
            clear=True,
        ):
            self.assertEqual(runner._tcp_ip_bulk_min_strict_budget_sec(self._BC_10MIB), 120)

    def test_min_goodput_wall_skipped_for_huge_bulk(self):
        with mock.patch.dict(
            os.environ,
            self._bulk_harness_env(MASQUE_STAND_MIN_GOODPUT_MBPS="1"),
            clear=True,
        ):
            w = runner._bulk_min_goodput_wall_sec(runner.BYTES_500MB)
        self.assertEqual(w, 0)


if __name__ == "__main__":
    unittest.main()
