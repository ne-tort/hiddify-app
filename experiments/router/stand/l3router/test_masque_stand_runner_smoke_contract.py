import json
import os
import tempfile
import unittest
from pathlib import Path
from unittest import mock

import masque_stand_runner as runner


class TestSmokeContractArtifacts(unittest.TestCase):
    def test_extract_writer_pids_from_probe_deduplicates_and_filters(self):
        pids = runner._extract_writer_pids_from_probe(
            "123 socat -u ...\n"
            "124 timeout 10 socat ...\n"
            "123 socat -u ...\n"
            "999 sh -lc pgrep -af 'socat.*ip-connect-ip-python\\.bin'\n"
            "badline\n"
            "0 ignored"
        )
        self.assertEqual(pids, [123, 124])

    def test_stand_tcp_ip_udp_chunk_honors_cli_size(self):
        with mock.patch.dict(os.environ, {}, clear=True):
            raw, effective = runner._stand_tcp_ip_udp_chunk(1000)
        self.assertEqual(raw, 1000)
        self.assertEqual(effective, 1000)

    def test_stand_tcp_ip_udp_chunk_honors_shared_env_and_cap(self):
        with mock.patch.dict(
            os.environ,
            {
                "MASQUE_STAND_UDP_CHUNK": "1000",
                "MASQUE_TCP_IP_UDP_PAYLOAD_CAP": "900",
            },
            clear=True,
        ):
            raw, effective = runner._stand_tcp_ip_udp_chunk(0)
        self.assertEqual(raw, 1000)
        self.assertEqual(effective, 900)

    def test_stand_tcp_ip_udp_chunk_keeps_legacy_datagram_override(self):
        with mock.patch.dict(
            os.environ,
            {
                "MASQUE_TCP_IP_DATAGRAM": "800",
                "MASQUE_STAND_UDP_CHUNK": "1000",
            },
            clear=True,
        ):
            raw, effective = runner._stand_tcp_ip_udp_chunk(1100)
        self.assertEqual(raw, 800)
        self.assertEqual(effective, 800)

    def test_classify_error_route_guard_maps_to_policy(self):
        self.assertEqual(
            runner.classify_error("RuntimeError: route_guard_target_not_on_tun0"),
            "policy",
        )

    def test_collect_tcp_ip_sink_udp_diag_collects_probe_outputs(self):
        with mock.patch.object(
            runner,
            "docker_exec_capture",
            side_effect=[
                "ss",
                "netstat",
                "Udp: InDatagrams NoPorts InErrors OutDatagrams RcvbufErrors SndbufErrors\nUdp: 100 0 0 0 0 0",
                "probe-out",
                "probe-out",
                "probe-out",
                "probe-out",
                "probe-out",
                "Udp: InDatagrams NoPorts InErrors OutDatagrams RcvbufErrors SndbufErrors\nUdp: 104 0 0 0 0 0",
            ],
        ) as capture, mock.patch.object(
            runner,
            "_collect_writer_proc_sample",
            side_effect=[
                {"captured": True, "sink_file_stat": "s1"},
                {"captured": True, "sink_file_stat": "s2"},
            ],
        ) as collect_writer:
            diag = runner._collect_tcp_ip_sink_udp_diag("docker")
        self.assertTrue(diag["captured"])
        self.assertEqual(diag["ss_udp"], "ss")
        self.assertEqual(diag["netstat_udp"], "netstat")
        self.assertIn("Udp:", diag["udp_snmp"])
        self.assertEqual(diag["sink_file_stat"], "probe-out")
        self.assertEqual(diag["socat_log_tail"], "probe-out")
        self.assertEqual(diag["writer_processes"], "probe-out")
        self.assertEqual(diag["writer_timeout_processes"], "probe-out")
        self.assertEqual(diag["writer_process_probe"], "probe-out")
        self.assertIn("udp_snmp_sample_1", diag)
        self.assertIn("udp_snmp_sample_2", diag)
        self.assertIn("udp_snmp_progress", diag)
        self.assertEqual(diag["udp_snmp_progress"]["delta_in_datagrams"], 4)
        self.assertEqual(diag["udp_snmp_progress"]["delta_in_errors"], 0)
        self.assertIn("sample_1", diag["writer_samples"])
        self.assertIn("sample_2", diag["writer_samples"])
        self.assertIn("writer_summary", diag)
        self.assertEqual(diag["writer_summary"]["sink_writer_progress_bytes"], 0)
        self.assertNotIn("errors", diag)
        self.assertEqual(capture.call_count, 9)
        self.assertEqual(collect_writer.call_count, 2)

    def test_collect_tcp_ip_sink_udp_diag_keeps_errors_non_fatal(self):
        with mock.patch.object(
            runner,
            "docker_exec_capture",
            side_effect=RuntimeError("probe failed"),
        ), mock.patch.object(
            runner,
            "_collect_writer_proc_sample",
            return_value={"captured": False},
        ):
            diag = runner._collect_tcp_ip_sink_udp_diag("docker")
        self.assertFalse(diag["captured"])
        self.assertIn("errors", diag)
        self.assertIn("ss_udp", diag["errors"])

    def test_parse_udp_snmp_error_counters_extracts_inerrors_and_rcvbuf(self):
        parsed = runner._parse_udp_snmp_error_counters(
            "Udp: InDatagrams NoPorts InErrors OutDatagrams RcvbufErrors SndbufErrors\n"
            "Udp: 17845 0 0 17846 0 0"
        )
        self.assertTrue(parsed["parsed"])
        self.assertEqual(parsed["in_datagrams"], 17845)
        self.assertEqual(parsed["in_errors"], 0)
        self.assertEqual(parsed["rcvbuf_errors"], 0)

    def test_classify_sink_udp_ingress_datagram_gap_no_udp_errors(self):
        signal = runner._classify_sink_writer_boundary_signal(
            settled=False,
            late_growth_bytes=0,
            sink_udp_diag={
                "udp_snmp": (
                    "Udp: InDatagrams NoPorts InErrors OutDatagrams RcvbufErrors SndbufErrors\n"
                    "Udp: 20956 0 0 0 0 0"
                )
            },
            expected_datagrams=20972,
        )
        self.assertEqual(signal, "sink_udp_ingress_datagram_gap_no_udp_errors")

    def test_classify_sink_udp_gap_prefers_snmp_progress_delta_for_current_run(self):
        signal = runner._classify_sink_writer_boundary_signal(
            settled=False,
            late_growth_bytes=0,
            sink_udp_diag={
                "udp_snmp": (
                    "Udp: InDatagrams NoPorts InErrors OutDatagrams RcvbufErrors SndbufErrors\n"
                    "Udp: 999999 0 0 0 0 0"
                ),
                "udp_snmp_progress": {
                    "delta_in_datagrams": 20956,
                    "delta_in_errors": 0,
                },
            },
            expected_datagrams=20972,
        )
        self.assertEqual(signal, "sink_udp_ingress_datagram_gap_no_udp_errors")

    def test_classify_sink_writer_boundary_signal_no_udp_errors(self):
        signal = runner._classify_sink_writer_boundary_signal(
            settled=False,
            late_growth_bytes=0,
            sink_udp_diag={
                "udp_snmp": (
                    "Udp: InDatagrams NoPorts InErrors OutDatagrams RcvbufErrors SndbufErrors\n"
                    "Udp: 17845 0 0 17846 0 0"
                )
            },
        )
        self.assertEqual(signal, "sink_writer_boundary_no_udp_errors")

    def test_classify_sink_writer_boundary_signal_process_absent(self):
        signal = runner._classify_sink_writer_boundary_signal(
            settled=False,
            late_growth_bytes=0,
            sink_udp_diag={
                "captured": True,
                "writer_processes": " ",
                "writer_timeout_processes": "",
                "writer_process_probe": "",
                "udp_snmp": (
                    "Udp: InDatagrams NoPorts InErrors OutDatagrams RcvbufErrors SndbufErrors\n"
                    "Udp: 17845 0 0 17846 0 0"
                ),
            },
        )
        self.assertEqual(signal, "sink_writer_process_absent")

    def test_classify_sink_writer_process_absent_requires_empty_probe(self):
        signal = runner._classify_sink_writer_boundary_signal(
            settled=False,
            late_growth_bytes=0,
            sink_udp_diag={
                "captured": True,
                "writer_processes": "",
                "writer_timeout_processes": "",
                "writer_process_probe": "123 socat ...",
                "udp_snmp": (
                    "Udp: InDatagrams NoPorts InErrors OutDatagrams RcvbufErrors SndbufErrors\n"
                    "Udp: 17845 0 0 17846 0 0"
                ),
            },
        )
        self.assertEqual(signal, "sink_writer_boundary_no_udp_errors")

    def test_classify_sink_writer_boundary_signal_requires_stalled_and_unsettled(self):
        signal = runner._classify_sink_writer_boundary_signal(
            settled=True,
            late_growth_bytes=0,
            sink_udp_diag={
                "udp_snmp": (
                    "Udp: InDatagrams NoPorts InErrors OutDatagrams RcvbufErrors SndbufErrors\n"
                    "Udp: 17845 0 0 17846 0 0"
                )
            },
        )
        self.assertIsNone(signal)

    def test_should_collect_sink_udp_diag_for_bridge_signals(self):
        self.assertTrue(
            runner._should_collect_sink_udp_diag(
                stop_reason="bridge_boundary_stall",
                got=1024,
                expected=4096,
            )
        )

    def test_should_collect_sink_udp_diag_for_near_full_budget_exceeded(self):
        self.assertTrue(
            runner._should_collect_sink_udp_diag(
                stop_reason="budget_exceeded",
                got=20960000,
                expected=20971520,
            )
        )

    def test_should_collect_sink_udp_diag_skips_non_near_full_budget_exceeded(self):
        self.assertFalse(
            runner._should_collect_sink_udp_diag(
                stop_reason="budget_exceeded",
                got=10000000,
                expected=20971520,
            )
        )

    def test_summarize_sink_writer_samples_extracts_size_mtime_and_proc_delta(self):
        summary = runner._summarize_sink_writer_samples(
            {
                "writer_samples": {
                    "sample_1": {
                        "sink_file_stat": (
                            "100 /tmp/ip-connect-ip-python.bin\n"
                            "sink_file_size_bytes=100 sink_file_mtime_ns=10"
                        ),
                        "proc_io": {"12": "write_bytes: 1000"},
                    },
                    "sample_2": {
                        "sink_file_stat": (
                            "164 /tmp/ip-connect-ip-python.bin\n"
                            "sink_file_size_bytes=164 sink_file_mtime_ns=42"
                        ),
                        "proc_io": {"12": "write_bytes: 1096"},
                    },
                }
            }
        )
        self.assertEqual(summary["sample_1"]["sink_file_size_bytes"], 100)
        self.assertEqual(summary["sample_2"]["sink_file_size_bytes"], 164)
        self.assertEqual(summary["sink_writer_progress_bytes"], 64)
        self.assertEqual(summary["sink_writer_progress_mtime_ns"], 32)
        self.assertEqual(summary["sink_writer_progress_proc_write_bytes"], 96)
        self.assertEqual(summary["writer_idle_vs_blocked"], "writer_progressing")

    def test_classify_writer_idle_vs_blocked_idle(self):
        self.assertEqual(runner._classify_writer_idle_vs_blocked(0, 0, 0), "idle_no_progress")

    def test_classify_writer_idle_vs_blocked_blocked_after_write(self):
        self.assertEqual(runner._classify_writer_idle_vs_blocked(0, 0, 64), "blocked_after_write")

    def test_obs_nested_counter_extracts_int_or_zero(self):
        self.assertEqual(
            runner._obs_nested_counter(
                {"quic_datagram_tx_path_total": {"sendmsg_ok": 7}},
                "quic_datagram_tx_path_total",
                "sendmsg_ok",
            ),
            7,
        )
        self.assertEqual(runner._obs_nested_counter({}, "quic_datagram_tx_path_total", "sendmsg_ok"), 0)

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

    def test_classify_tcp_ip_stop_reason_near_full_under_cadence(self):
        # `maybeEmitConnectIPActiveSnapshot` throttles to 1s per emit, so a
        # sub-second bulk window with `bytes_received` near full and only one
        # observability emit must not be classified as a hard `bridge_boundary_stall`.
        obs = {
            "source": "runtime_snapshot_log_marker",
            "delta": {
                "connect_ip_bridge_udp_tx_attempt_total": 1,
                "connect_ip_packet_tx_total": 1,
                "connect_ip_packet_rx_total": 0,
                "connect_ip_engine_ingress_total": 0,
            },
        }
        reason = runner.classify_tcp_ip_stop_reason(
            send_err=None,
            got=20948080,
            expected=20971520,
            hash_ok=False,
            settled=True,
            budget_exceeded=False,
            obs=obs,
        )
        self.assertEqual(reason, "near_full_loss_under_cadence")

    def test_classify_tcp_ip_stop_reason_route_guard_is_typed(self):
        reason = runner.classify_tcp_ip_stop_reason(
            send_err="RuntimeError: route_guard_target_not_on_tun0",
            got=0,
            expected=20971520,
            hash_ok=False,
            settled=False,
            budget_exceeded=True,
            obs={},
        )
        self.assertEqual(reason, "route_guard_target_not_on_tun0")

    def test_classify_tcp_ip_stop_reason_near_full_micro_loss_at_deadline(self):
        obs = {
            "source": "runtime_snapshot_log_marker",
            "delta": {
                "connect_ip_bridge_udp_tx_attempt_total": 2,
                "connect_ip_packet_tx_total": 2,
                "connect_ip_packet_rx_total": 0,
                "connect_ip_engine_ingress_total": 0,
            },
        }
        reason = runner.classify_tcp_ip_stop_reason(
            send_err=None,
            got=20948080,
            expected=20971520,
            hash_ok=False,
            settled=True,
            budget_exceeded=True,
            obs=obs,
            budget_margin_sec=-0.15,
        )
        self.assertEqual(reason, "near_full_micro_loss_at_deadline")

    def test_classify_tcp_ip_stop_reason_near_full_micro_loss_requires_near_deadline_budget(self):
        obs = {
            "source": "runtime_snapshot_log_marker",
            "delta": {
                "connect_ip_bridge_udp_tx_attempt_total": 2,
                "connect_ip_packet_tx_total": 2,
                "connect_ip_packet_rx_total": 0,
                "connect_ip_engine_ingress_total": 0,
            },
        }
        reason = runner.classify_tcp_ip_stop_reason(
            send_err=None,
            got=20948080,
            expected=20971520,
            hash_ok=False,
            settled=True,
            budget_exceeded=True,
            obs=obs,
            budget_margin_sec=-3.2,
        )
        self.assertEqual(reason, "near_full_loss_under_cadence")

    def test_connect_ip_cadence_sparse_signal_marks_full_delivery_with_thin_obs(self):
        obs = {
            "observability_peer_split": True,
            "delta": {
                "connect_ip_bridge_udp_tx_attempt_total": 1,
                "connect_ip_packet_tx_total": 1,
                "connect_ip_packet_rx_total": 0,
                "quic_datagram_tx_path_total": {"sendmsg_ok": 0},
                "quic_datagram_post_decrypt_path_total": {"short_unpack_ok": 0},
            },
        }
        signal = runner._connect_ip_cadence_sparse_signal(obs, got=20971520, expected=20971520)
        self.assertTrue(signal["flag"])
        self.assertEqual(signal["reason"], "cadence_sparse")

    def test_connect_ip_cadence_sparse_signal_skips_partial_delivery(self):
        obs = {
            "delta": {
                "connect_ip_bridge_udp_tx_attempt_total": 1,
                "connect_ip_packet_tx_total": 1,
                "connect_ip_packet_rx_total": 0,
            },
        }
        signal = runner._connect_ip_cadence_sparse_signal(obs, got=2048, expected=4096)
        self.assertFalse(signal["flag"])
        self.assertEqual(signal["reason"], "delivery_not_full")

    def test_ensure_tun_route_for_tcp_ip_detects_tun0(self):
        with mock.patch.object(
            runner,
            "docker_exec_capture",
            side_effect=["", "10.200.0.2 dev tun0 src 10.0.0.2 uid 0"],
        ) as capture:
            probe, on_tun0 = runner._ensure_tun_route_for_tcp_ip("docker", "10.200.0.2")
        self.assertTrue(on_tun0)
        self.assertIn("dev tun0", probe)
        self.assertEqual(capture.call_count, 2)

    def test_read_tun0_dev_bytes_parses_rx_tx(self):
        with mock.patch.object(
            runner,
            "docker_exec_capture",
            return_value="12345 67890",
        ):
            stats = runner._read_tun0_dev_bytes("docker")
        self.assertTrue(stats["ok"])
        self.assertEqual(stats["rx_bytes"], 12345)
        self.assertEqual(stats["tx_bytes"], 67890)

    def test_read_tun0_dev_bytes_handles_missing_tun(self):
        with mock.patch.object(
            runner,
            "docker_exec_capture",
            return_value="",
        ):
            stats = runner._read_tun0_dev_bytes("docker")
        self.assertFalse(stats["ok"])
        self.assertEqual(stats["rx_bytes"], 0)
        self.assertEqual(stats["tx_bytes"], 0)

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


class TestTcpIpPacedTailHelpers(unittest.TestCase):
    def test_receive_tail_cap_ci_neutral(self):
        with mock.patch.dict(os.environ, {}, clear=True):
            self.assertEqual(
                runner._tcp_ip_receive_phase_tail_cap_sec(runner.BULK_TCP_IP_NEAR_FULL_HIGH_RATE_BPS_THRESHOLD),
                runner.BULK_SINGLE_FLOW_RECEIVE_TAIL_CAP_SEC,
            )

    def test_receive_tail_cap_slow_high_rate(self):
        with mock.patch.dict(os.environ, {"MASQUE_STAND_SLOW_DOCKER": "1"}, clear=True):
            self.assertEqual(
                runner._tcp_ip_receive_phase_tail_cap_sec(runner.BULK_TCP_IP_NEAR_FULL_HIGH_RATE_BPS_THRESHOLD),
                min(120.0, float(runner.BULK_SINGLE_FLOW_RECEIVE_TAIL_CAP_SEC) + 60.0),
            )

    def test_near_full_extra_cap_slow_high_rate(self):
        with mock.patch.dict(os.environ, {"MASQUE_STAND_SLOW_DOCKER": "1"}, clear=True):
            want = min(
                120.0,
                float(runner.BULK_TCP_IP_NEAR_FULL_RECV_CAP_SEC) + 30.0,
            )
            self.assertEqual(
                runner._tcp_ip_near_full_extra_cap_sec(runner.BULK_TCP_IP_NEAR_FULL_HIGH_RATE_BPS_THRESHOLD),
                want,
            )

    def test_phase_slack_slow_high_rate_uses_tail_cap(self):
        """Regression: linear slack (12 + 0.2 * strict) must not clamp below raised tail cap."""
        strict_20mib = 240  # 20 MiB * 12 s/MiB under slow profile
        rate = 140_000_000
        with mock.patch.dict(os.environ, {"MASQUE_STAND_SLOW_DOCKER": "1"}, clear=True):
            slack = runner._tcp_ip_receive_phase_slack_sec(strict_20mib, rate)
            self.assertEqual(
                slack,
                runner._tcp_ip_receive_phase_tail_cap_sec(rate),
            )
        self.assertGreater(slack, 12.0 + strict_20mib * runner.BULK_SINGLE_FLOW_RECEIVE_TAIL_PER_STRICT_SEC)

    def test_phase_slack_ci_neutral_high_rate(self):
        with mock.patch.dict(os.environ, {}, clear=True):
            slack = runner._tcp_ip_receive_phase_slack_sec(240, 140_000_000)
        self.assertEqual(slack, 60.0)

    def test_socket_buf_ci_neutral_default(self):
        with mock.patch.dict(os.environ, {}, clear=True):
            buf_bytes = runner._tcp_ip_socket_buf_bytes(140_000_000)
        self.assertEqual(buf_bytes, runner.BULK_TCP_IP_SOCKET_BUF_DEFAULT)

    def test_socket_buf_slow_high_rate(self):
        with mock.patch.dict(os.environ, {"MASQUE_STAND_SLOW_DOCKER": "1"}, clear=True):
            buf_bytes = runner._tcp_ip_socket_buf_bytes(runner.BULK_TCP_IP_NEAR_FULL_HIGH_RATE_BPS_THRESHOLD)
        self.assertEqual(buf_bytes, runner.BULK_TCP_IP_SOCKET_BUF_SLOW_HIGH_RATE)

    def test_socket_buf_env_override_clamped(self):
        with mock.patch.dict(
            os.environ,
            {"MASQUE_TCP_IP_SOCKET_BUF_BYTES": str(runner.BULK_TCP_IP_SOCKET_BUF_MAX * 2)},
            clear=True,
        ):
            buf_bytes = runner._tcp_ip_socket_buf_bytes(0)
        self.assertEqual(buf_bytes, runner.BULK_TCP_IP_SOCKET_BUF_MAX)


class TestTcpIpSmokeDeadlineThroughputGate(unittest.TestCase):
    def test_applies_smoke_deadline_for_10kb(self):
        ok = runner._apply_tcp_ip_smoke_deadline_to_throughput_ok(
            throughput_ok=True,
            send_elapsed=runner.SMOKE_DEADLINE_SEC + 0.1,
            byte_count=runner.BYTES_10KB,
        )
        self.assertFalse(ok)

    def test_does_not_apply_smoke_deadline_for_non_smoke_payload(self):
        ok = runner._apply_tcp_ip_smoke_deadline_to_throughput_ok(
            throughput_ok=True,
            send_elapsed=runner.SMOKE_DEADLINE_SEC + 5.0,
            byte_count=runner.BYTES_10KB * 2,
        )
        self.assertTrue(ok)


if __name__ == "__main__":
    unittest.main()
