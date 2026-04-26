import 'dart:convert';

import 'package:hiddify/core/localization/translations.dart';
import 'package:hiddify/core/model/failures.dart';
import 'package:hiddify/core/notification/in_app_notification_controller.dart';
import 'package:hiddify/core/router/dialog/dialog_notifier.dart';
import 'package:hiddify/features/profile/data/profile_data_providers.dart';
import 'package:hiddify/features/profile/data/profile_repository.dart';
import 'package:hiddify/features/profile/details/profile_details_state.dart';
import 'package:hiddify/features/profile/model/profile_entity.dart';
import 'package:hiddify/features/profile/model/profile_failure.dart';
import 'package:hiddify/utils/utils.dart';
import 'package:riverpod_annotation/riverpod_annotation.dart';

part 'profile_details_notifier.g.dart';

@riverpod
class ProfileDetailsNotifier extends _$ProfileDetailsNotifier with AppLogger {
  ProfileRepository get _profilesRepo => ref.read(profileRepositoryProvider).requireValue;

  static List<dynamic> _toDynamicList(dynamic raw) {
    if (raw == null) return <dynamic>[];
    if (raw is List) return List<dynamic>.from(raw);
    return <dynamic>[raw];
  }

  static void _normalizeListableField(Map<String, dynamic> node, String key) {
    if (!node.containsKey(key)) return;
    node[key] = _toDynamicList(node[key]);
  }

  // Keep listable network fields stable in editor/save flow:
  // string <-> list flips can trigger client-side crashes in generic JSON tree widgets.
  static Map<String, dynamic> _normalizeProfileJson(Map<String, dynamic> root) {
    final clone = Map<String, dynamic>.from(root);
    final endpoints = _toDynamicList(clone['endpoints']);
    for (var i = 0; i < endpoints.length; i++) {
      final e = endpoints[i];
      if (e is! Map) continue;
      final ep = Map<String, dynamic>.from(e as Map);
      _normalizeListableField(ep, 'address');
      final peers = _toDynamicList(ep['peers']);
      for (var i = 0; i < peers.length; i++) {
        final pRaw = peers[i];
        if (pRaw is! Map) continue;
        final p = Map<String, dynamic>.from(pRaw as Map);
        _normalizeListableField(p, 'allowed_ips');
        peers[i] = p;
      }
      ep['peers'] = peers;
      endpoints[i] = ep;
    }
    clone['endpoints'] = endpoints;
    final inbounds = _toDynamicList(clone['inbounds']);
    for (var i = 0; i < inbounds.length; i++) {
      final raw = inbounds[i];
      if (raw is! Map) continue;
      final inbound = Map<String, dynamic>.from(raw as Map);
      _normalizeListableField(inbound, 'address');
      inbounds[i] = inbound;
    }
    clone['inbounds'] = inbounds;
    return clone;
  }

  @override
  Future<ProfileDetailsState> build(String id) async {
    final prof = (await _profilesRepo.getById(id).run()).match((l) => throw l, (prof) {
      // _originalProfile = prof;
      if (prof == null) {
        loggy.warning('profile with id: [$id] does not exist');
        throw const ProfileNotFoundFailure();
      }
      return prof;
    });
    var profContent = "";
    try {
      profContent = (await _profilesRepo.generateConfig(id).run()).match(
        (l) => throw Exception('Failed to generate config: $l'),
        (content) => content,
      );
    } catch (e, st) {
      loggy.error('Error generating config for profile $id', e, st);
      // Optionally, you can set profContent to an empty string or keep the original content
      profContent = await _profilesRepo.getRawConfig(id).run().then((e) => e.getOrElse((f) => ""));
    }
    try {
      final decoded = jsonDecode(profContent);
      final jsonObject = decoded is Map<String, dynamic> ? _normalizeProfileJson(decoded) : <String, dynamic>{};
      final List<Map<String, dynamic>> outbounds = [];
      if (jsonObject is Map<String, dynamic> && jsonObject['outbounds'] is List) {
        for (final outbound in jsonObject['outbounds'] as List<dynamic>) {
          if (outbound is Map<String, dynamic> &&
              outbound['type'] != null &&
              !['selector', 'urltest', 'dns', 'block', 'balancer'].contains(outbound['type']) &&
              !['direct', 'bypass', 'direct-fragment'].contains(outbound['tag'])) {
            outbounds.add(outbound);
          }
        }
      } else {
        // print('No outbounds found in the config');
      }
      final endpoints = jsonObject['endpoints'] as List? ?? [];
      final inbounds = jsonObject['inbounds'] as List? ?? [];
      profContent =
          '{"outbounds": ${json.encode(outbounds)},"endpoints":${json.encode(endpoints)},"inbounds":${json.encode(inbounds)} }';
      loggy.info("profile details config prepared (len=${profContent.length})");
    } catch (e, st) {
      loggy.error('Error parsing profile-content JSON', e, st);
      // rethrow;
    }
    return ProfileDetailsState(
      loadingState: const AsyncData(null),
      profile: prof,
      configContent: profContent,
      isDetailsChanged: false,
    );
  }

  Future<T?> doAsync<T>(Future<T> Function() operation) async {
    if (state case AsyncData(value: final ProfileDetailsState data)) {
      state = AsyncData(data.copyWith(loadingState: const AsyncLoading()));
      final T? result = await operation();
      state = AsyncData(data.copyWith(loadingState: const AsyncData(null)));
      return result;
    }
    return null;
  }

  void setUserOverride(UserOverride userOverride) {
    if (state case AsyncData(value: final ProfileDetailsState data)) {
      state = AsyncData(
        data.copyWith(profile: data.profile.copyWith(userOverride: userOverride), isDetailsChanged: true),
      );
    }
  }

  void setContent(String configContent) {
    if (state case AsyncData(value: final ProfileDetailsState data)) {
      String normalized = configContent;
      try {
        final decoded = jsonDecode(configContent);
        if (decoded is Map<String, dynamic>) {
          normalized = const JsonEncoder.withIndent('  ').convert(_normalizeProfileJson(decoded));
        }
      } catch (_) {}
      state = AsyncData(data.copyWith(configContent: normalized, isDetailsChanged: true));
    }
  }

  Future<bool> save() async {
    bool success = false;
    if (state case AsyncData(:final value)) {
      if (value.loadingState case AsyncLoading()) return false;

      success =
          await doAsync<bool>(() async {
            final t = await ref.read(translationsProvider.future);
            var normalizedContent = value.configContent;
            try {
              final decoded = jsonDecode(value.configContent);
              if (decoded is Map<String, dynamic>) {
                normalizedContent = const JsonEncoder.withIndent('  ').convert(_normalizeProfileJson(decoded));
              }
            } catch (_) {}
            return (await _profilesRepo.offlineUpdate(value.profile, normalizedContent).run()).match(
              (l) async {
                await ref
                    .read(dialogNotifierProvider.notifier)
                    .showCustomAlertFromErr(
                      t.presentError(l, action: t.pages.profiles.msg.update.failureNamed(name: value.profile.name)),
                    );
                return false;
              },
              (r) {
                ref.read(inAppNotificationControllerProvider).showSuccessToast(t.pages.profiles.msg.update.success);
                return true;
              },
            );
          }) ??
          false;
    }
    return success;
  }
}
