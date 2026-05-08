import json
import unittest

from masque_runtime_ci_gate_asserts import (
    MANDATORY_CHECKS,
    _assert_runtime_contract_connect_ip_post_send_remote_visibility_correlation,
    _merge_post_anti_bypass_summary,
)


def _checks_with_row(ok=True, active=True, stop_reason="post_send_frame_visibility_absent"):
    return {
        "summary": {
            "connect_ip_post_send_remote_visibility_correlation": {
                "ok": ok,
                "active": active,
                "stop_reason": stop_reason,
            }
        }
    }


class TestConnectIPPostSendRemoteVisibilityCorrelationAssert(unittest.TestCase):
    def test_green_when_row_matches_typed_contract(self):
        failures = _assert_runtime_contract_connect_ip_post_send_remote_visibility_correlation(
            _checks_with_row(ok=True, active=True, stop_reason="post_send_frame_visibility_absent")
        )
        self.assertEqual(failures, [])

    def test_red_when_stop_reason_mismatch(self):
        failures = _assert_runtime_contract_connect_ip_post_send_remote_visibility_correlation(
            _checks_with_row(ok=True, active=True, stop_reason="receiver_incomplete")
        )
        self.assertTrue(
            any("stop_reason" in failure for failure in failures),
            msg=f"expected stop_reason failure, got: {failures}",
        )

    def test_green_when_row_not_active_skips_correlation_strictness(self):
        """Inactive row: no strict stop_reason/ok enforcement (green/scoped artifacts)."""
        failures = _assert_runtime_contract_connect_ip_post_send_remote_visibility_correlation(
            _checks_with_row(ok=True, active=False, stop_reason="post_send_frame_visibility_absent")
        )
        self.assertEqual(failures, [])

    def test_green_when_row_has_extra_observability_fields(self):
        checks = _checks_with_row(ok=True, active=True, stop_reason="post_send_frame_visibility_absent")
        checks["summary"]["connect_ip_post_send_remote_visibility_correlation"].update(
            {
                "effective_udp_send_bps": 4_000_000,
                "udp_send_bps_source": "default",
            }
        )
        failures = _assert_runtime_contract_connect_ip_post_send_remote_visibility_correlation(checks)
        self.assertEqual(failures, [])

    def test_mandatory_checks_include_remote_visibility_row(self):
        self.assertIn(
            ("summary", "connect_ip_post_send_remote_visibility_correlation.ok", True),
            MANDATORY_CHECKS,
        )


class TestMergePostAntiBypassSummary(unittest.TestCase):
    def test_keeps_summary_red_when_non_anti_rows_absent(self):
        backup = {
            "ok": False,
            "results": [{"scenario": "tcp_stream", "ok": False, "error_class": "transport_init"}],
        }
        negative = {
            "tcp_stream": {"scenario": "tcp_stream", "ok": False, "error_class": "transport_init"},
        }
        merged = _merge_post_anti_bypass_summary(json.dumps(backup), negative)
        self.assertFalse(merged["ok"])

    def test_requires_non_anti_rows_to_mark_summary_green(self):
        backup = {
            "ok": False,
            "results": [{"scenario": "tcp_ip", "ok": False, "error_class": "transport_init"}],
        }
        negative = {
            "tcp_ip": {"scenario": "tcp_ip", "ok": False, "error_class": "transport_init"},
        }
        merged = _merge_post_anti_bypass_summary(json.dumps(backup), negative)
        self.assertFalse(merged["ok"])


if __name__ == "__main__":
    unittest.main()
