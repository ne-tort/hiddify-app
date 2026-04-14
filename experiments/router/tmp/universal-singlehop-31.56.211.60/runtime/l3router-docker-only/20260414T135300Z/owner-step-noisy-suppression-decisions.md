# owner-step noisy suppression decisions

- trend_regression_budget_trigger_key: none
- noisy_min_obs: 3
- noisy_flip_ratio_threshold_pct: 60
- noisy_age_decay_pct: 20
- noisy_age_min_weight_pct: 30
- decision_rows: 0
- suppressed_rows: 0
- kept_rows: 0
- trigger_match_suppressed_rows: 0
- trigger_match_kept_rows: 0

- trend_regression_budget_trigger_crosscheck_rows: 0
- trend_regression_budget_trigger_crosscheck_suppressed_rows: 0
- trend_regression_budget_trigger_crosscheck_kept_rows: 0

## owner-step suppression trend-age stability anomaly clustering

- owner_step_suppression_anomaly_cluster_total: 0
- owner_step_suppression_anomaly_cluster_stable_count: 0
- owner_step_suppression_anomaly_cluster_stable_min_obs: 3
- owner_step_suppression_anomaly_cluster_stable_min_weighted_flip_ratio_pct: 40
- owner_step_suppression_anomaly_cluster_stable_trace: none
- owner_step_suppression_anomaly_cluster_rca_top: none

| cluster_key | owner_step | guard_reason | control_plane_result | stable | obs_sum | weighted_flip_ratio_avg_pct | trigger_hits | suppressed_hits | kept_hits | owner_step_impact |
| --- | --- | --- | --- | ---: | ---: | ---: | ---: | ---: | ---: | --- |
| none | none | none | none | 0 | 0 | 0 | 0 | 0 | 0 | none |

## owner-step trend-age stability gate

- owner_step_guard_control_plane_trend_age_stability_gate_required: 1
- owner_step_guard_control_plane_trend_age_stability_shift_threshold_pct: 25
- owner_step_guard_control_plane_trend_age_stability_min_anomaly_weighted_ratio_pct: 60
- owner_step_guard_control_plane_trend_age_stability_min_series: 3
- owner_step_guard_control_plane_trend_age_stability_status: 0
- owner_step_guard_control_plane_trend_age_stability_trigger_owner_step: none
- owner_step_guard_control_plane_trend_age_stability_trigger_shift_pct: 0
- owner_step_guard_control_plane_trend_age_stability_trigger_weighted_flip_ratio_all_pct: 0
- owner_step_guard_control_plane_trend_age_stability_trigger_weighted_flip_ratio_anomaly_no_jitter_pct: 0
- owner_step_guard_control_plane_trend_age_stability_trigger_series_hits: 0
- owner_step_guard_control_plane_trend_age_stability_trigger_trace: none
- owner_step_guard_control_plane_trend_age_stability_per_step_trace: owner-a(all=0%,anomaly_no_jitter=0%,shift=0%,series_hits=0),owner-b(all=0%,anomaly_no_jitter=0%,shift=0%,series_hits=0),owner-c(all=0%,anomaly_no_jitter=0%,shift=0%,series_hits=0)

- owner_step_guard_control_plane_trend_age_stability_per_step_trigger_detail_trace: owner-a(gate_pass=0,shift_pass=0,anomaly_ratio_pass=0,series_pass=0,shift=0%/thr=25%/gap=-25%,anomaly=0%/thr=60%/gap=-60%,series=0/thr=3/gap=-3),owner-b(gate_pass=0,shift_pass=0,anomaly_ratio_pass=0,series_pass=0,shift=0%/thr=25%/gap=-25%,anomaly=0%/thr=60%/gap=-60%,series=0/thr=3/gap=-3),owner-c(gate_pass=0,shift_pass=0,anomaly_ratio_pass=0,series_pass=0,shift=0%/thr=25%/gap=-25%,anomaly=0%/thr=60%/gap=-60%,series=0/thr=3/gap=-3)

## owner-step trend-age stability auto-calibration

- owner_step_guard_control_plane_trend_age_stability_auto_calibration_enabled: 1
- owner_step_guard_control_plane_trend_age_stability_auto_calibration_status: insufficient-window
- owner_step_guard_control_plane_trend_age_stability_auto_calibration_min_runs: 3
- owner_step_guard_control_plane_trend_age_stability_auto_calibration_window_runs_count: 2
- owner_step_guard_control_plane_trend_age_stability_auto_calibration_sample_owner_count: 0
- owner_step_guard_control_plane_trend_age_stability_auto_calibration_shift_margin_pct: 5
- owner_step_guard_control_plane_trend_age_stability_auto_calibration_anomaly_margin_pct: 5
- owner_step_guard_control_plane_trend_age_stability_auto_calibration_min_series_floor: 2
- owner_step_guard_control_plane_trend_age_stability_auto_calibration_recommended_shift_threshold_pct: 25
- owner_step_guard_control_plane_trend_age_stability_auto_calibration_recommended_min_anomaly_weighted_ratio_pct: 60
- owner_step_guard_control_plane_trend_age_stability_auto_calibration_recommended_min_series: 3
- owner_step_guard_control_plane_trend_age_stability_auto_calibration_shift_threshold_delta_pct: 0
- owner_step_guard_control_plane_trend_age_stability_auto_calibration_min_anomaly_weighted_ratio_delta_pct: 0
- owner_step_guard_control_plane_trend_age_stability_auto_calibration_min_series_delta: 0
- owner_step_guard_control_plane_trend_age_stability_auto_calibration_trace: window_runs=2 below min_runs=3

## owner-step trend-age stability recommendation rollout guard

- owner_step_guard_control_plane_trend_age_stability_rollout_guard_enabled: 1
- owner_step_guard_control_plane_trend_age_stability_rollout_guard_status: hold-auto-calibration-not-ready
- owner_step_guard_control_plane_trend_age_stability_rollout_guard_rollout_ready_signal: 0
- owner_step_guard_control_plane_trend_age_stability_rollout_guard_recommendation_changed: 0
- owner_step_guard_control_plane_trend_age_stability_rollout_guard_max_shift_delta_pct: 15
- owner_step_guard_control_plane_trend_age_stability_rollout_guard_max_anomaly_delta_pct: 15
- owner_step_guard_control_plane_trend_age_stability_rollout_guard_max_min_series_delta: 2
- owner_step_guard_control_plane_trend_age_stability_rollout_guard_impact_score: 0
- owner_step_guard_control_plane_trend_age_stability_rollout_guard_impact_level: low
- owner_step_guard_control_plane_trend_age_stability_rollout_guard_impact_preview: shift=25->25(delta=0),min_anomaly_weighted_ratio=60->60(delta=0),min_series=3->3(delta=0),impact_score=0,impact_level=low
- owner_step_guard_control_plane_trend_age_stability_rollout_guard_trace: status=hold-auto-calibration-not-ready,auto_calibration_status=insufficient-window,recommendation_changed=0,within_shift=1/15,within_anomaly=1/15,within_min_series=1/2

## owner-step trend-age stability recommendation confidence buckets

- owner_step_guard_control_plane_trend_age_stability_recommendation_confidence_bucket_high_count: 0
- owner_step_guard_control_plane_trend_age_stability_recommendation_confidence_bucket_medium_count: 0
- owner_step_guard_control_plane_trend_age_stability_recommendation_confidence_bucket_low_count: 3
- owner_step_guard_control_plane_trend_age_stability_recommendation_confidence_bucket_distribution: high:0,medium:0,low:3
- owner_step_guard_control_plane_trend_age_stability_recommendation_confidence_per_owner_step_trace: owner-a(bucket=low,gate_pass=0,shift_gap=-25,anomaly_gap=-60,series_gap=-3,rollout_ready=0,auto_calibration=insufficient-window,shift=0%,anomaly=0%,series_hits=0),owner-b(bucket=low,gate_pass=0,shift_gap=-25,anomaly_gap=-60,series_gap=-3,rollout_ready=0,auto_calibration=insufficient-window,shift=0%,anomaly=0%,series_hits=0),owner-c(bucket=low,gate_pass=0,shift_gap=-25,anomaly_gap=-60,series_gap=-3,rollout_ready=0,auto_calibration=insufficient-window,shift=0%,anomaly=0%,series_hits=0)
- owner_step_guard_control_plane_trend_age_stability_recommendation_confidence_top_owner_steps: owner-a(bucket=low,gate_pass=0,shift_gap=-25,anomaly_gap=-60,series_gap=-3,rollout_ready=0,auto_calibration=insufficient-window,shift=0%,anomaly=0%,series_hits=0),owner-b(bucket=low,gate_pass=0,shift_gap=-25,anomaly_gap=-60,series_gap=-3,rollout_ready=0,auto_calibration=insufficient-window,shift=0%,anomaly=0%,series_hits=0),owner-c(bucket=low,gate_pass=0,shift_gap=-25,anomaly_gap=-60,series_gap=-3,rollout_ready=0,auto_calibration=insufficient-window,shift=0%,anomaly=0%,series_hits=0)

## owner-step recommendation confidence drift audit

- owner_step_guard_control_plane_trend_age_stability_recommendation_confidence_drift_previous_run_found: 0
- owner_step_guard_control_plane_trend_age_stability_recommendation_confidence_drift_previous_run: none
- owner_step_guard_control_plane_trend_age_stability_recommendation_confidence_drift_current_run: none
- owner_step_guard_control_plane_trend_age_stability_recommendation_confidence_drift_bucket_shift_distribution: high:0,medium:0,low:0
- owner_step_guard_control_plane_trend_age_stability_recommendation_confidence_drift_top_bucket_shifts_per_owner_step: none
- owner_step_guard_control_plane_trend_age_stability_recommendation_confidence_drift_regression_hints: none

| owner_step | guard_reason | control_plane_result | decision | obs | flips | flip_ratio_pct | weighted_flip_ratio_pct | threshold_pct | min_obs | owner_step_impact | age_weight_profile |
| --- | --- | --- | --- | ---: | ---: | ---: | ---: | ---: | ---: | --- | --- |
| none | none | none | kept | 0 | 0 | 0 | 0 | 0 | 0 | none | none |
