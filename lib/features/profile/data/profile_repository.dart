import 'dart:convert';
import 'dart:io';

import 'package:dio/dio.dart';
import 'package:drift/drift.dart';
import 'package:flutter/foundation.dart';
import 'package:fpdart/fpdart.dart';
import 'package:hiddify/core/db/db.dart';

import 'package:hiddify/core/utils/exception_handler.dart';
import 'package:hiddify/features/profile/data/profile_data_mapper.dart';
import 'package:hiddify/features/profile/data/profile_data_source.dart';
import 'package:hiddify/features/profile/data/profile_parser.dart';
import 'package:hiddify/features/profile/data/profile_path_resolver.dart';
import 'package:hiddify/features/profile/model/profile_entity.dart';
import 'package:hiddify/features/profile/model/profile_failure.dart';
import 'package:hiddify/features/profile/model/profile_sort_enum.dart';
import 'package:hiddify/features/settings/data/config_option_repository.dart';
import 'package:hiddify/hiddifycore/hiddify_core_service.dart';
import 'package:hiddify/utils/custom_loggers.dart';
import 'package:uuid/uuid.dart';

abstract interface class ProfileRepository {
  TaskEither<ProfileFailure, Unit> init();
  TaskEither<ProfileFailure, ProfileEntity?> getById(String id);
  TaskEither<ProfileFailure, Unit> setAsActive(String id);
  TaskEither<ProfileFailure, Unit> deleteById(String id, bool isActive);
  Stream<Either<ProfileFailure, ProfileEntity?>> watchActiveProfile();
  Stream<Either<ProfileFailure, bool>> watchHasAnyProfile();
  Stream<Either<ProfileFailure, List<ProfileEntity>>> watchAll({
    ProfilesSort sort = ProfilesSort.lastUpdate,
    SortMode sortMode = SortMode.ascending,
  });
  TaskEither<ProfileFailure, Unit> upsertRemote(String url, {UserOverride? userOverride, CancelToken? cancelToken});
  TaskEither<ProfileFailure, Unit> addLocal(String content, {UserOverride? userOverride});
  TaskEither<ProfileFailure, Unit> offlineUpdate(ProfileEntity nProfile, String nContent);
  TaskEither<ProfileFailure, Unit> validateConfig(String path, String tempPath, String? profileOverride, bool debug);
  TaskEither<ProfileFailure, String> generateConfig(String id);
  TaskEither<ProfileFailure, String> getRawConfig(String id);
}

class ProfileRepositoryImpl with ExceptionHandler, InfraLogger implements ProfileRepository {
  Future<void> _sanitizeAwgSystemForWindows(String tempPath) async {
    if (!Platform.isWindows) return;
    final tempFile = File(tempPath);
    if (!await tempFile.exists()) return;
    Map<String, dynamic> root;
    try {
      final decoded = jsonDecode(await tempFile.readAsString());
      if (decoded is! Map<String, dynamic>) return;
      root = Map<String, dynamic>.from(decoded);
    } catch (_) {
      return;
    }
    final rawEndpoints = root['endpoints'];
    if (rawEndpoints is! List) return;
    var changed = false;
    final endpoints = List<dynamic>.from(rawEndpoints);
    for (var i = 0; i < endpoints.length; i++) {
      final raw = endpoints[i];
      if (raw is! Map) continue;
      final ep = Map<String, dynamic>.from(raw as Map);
      if (ep['type'] == 'awg' && ep['system'] == true) {
        // Temporary guard: system=true for AWG on Windows currently crashes client/core flow.
        ep['system'] = false;
        endpoints[i] = ep;
        changed = true;
      }
    }
    if (!changed) return;
    root['endpoints'] = endpoints;
    await tempFile.writeAsString(const JsonEncoder.withIndent('  ').convert(root));
    loggy.warning('AWG endpoint "system=true" sanitized to false on Windows to prevent crash');
  }

  ProfileRepositoryImpl({
    required ProfileDataSource profileDataSource,
    required ProfilePathResolver profilePathResolver,
    required HiddifyCoreService singbox,
    required ConfigOptionRepository configOptionRepository,
    required ProfileParser profileParser,
  }) : _profileParser = profileParser,
       _configOptionRepo = configOptionRepository,
       _singbox = singbox,
       _profilePathResolver = profilePathResolver,
       _profileDataSource = profileDataSource;

  final ProfileDataSource _profileDataSource;
  final ProfilePathResolver _profilePathResolver;
  final HiddifyCoreService _singbox;
  final ConfigOptionRepository _configOptionRepo;
  final ProfileParser _profileParser;

  @override
  TaskEither<ProfileFailure, Unit> init() {
    return exceptionHandler(() async {
      if (!kIsWeb) {
        if (!await _profilePathResolver.directory.exists()) {
          await _profilePathResolver.directory.create(recursive: true);
        }
      }

      return right(unit);
    }, ProfileUnexpectedFailure.new);
  }

  @override
  TaskEither<ProfileFailure, ProfileEntity?> getById(String id) {
    return TaskEither.tryCatch(
      () => _profileDataSource.getById(id).then((value) => value?.toEntity()),
      ProfileUnexpectedFailure.new,
    );
  }

  @override
  TaskEither<ProfileFailure, Unit> setAsActive(String id) {
    return TaskEither.tryCatch(() async {
      await _profileDataSource.edit(id, const ProfileEntriesCompanion(active: Value(true)));
      return unit;
    }, ProfileUnexpectedFailure.new);
  }

  @override
  TaskEither<ProfileFailure, Unit> deleteById(String id, bool isActive) {
    return TaskEither.tryCatch(() async {
      await _profileDataSource.deleteById(id, isActive);
      await _profilePathResolver.file(id).delete();
      return unit;
    }, ProfileUnexpectedFailure.new);
  }

  @override
  Stream<Either<ProfileFailure, ProfileEntity?>> watchActiveProfile() {
    return _profileDataSource.watchActiveProfile().map((event) => event?.toEntity()).handleExceptions((
      error,
      stackTrace,
    ) {
      loggy.error("error watching active profile", error, stackTrace);
      return ProfileUnexpectedFailure(error, stackTrace);
    });
  }

  @override
  Stream<Either<ProfileFailure, bool>> watchHasAnyProfile() {
    return _profileDataSource
        .watchProfilesCount()
        .map((event) => event != 0)
        .handleExceptions(ProfileUnexpectedFailure.new);
  }

  @override
  Stream<Either<ProfileFailure, List<ProfileEntity>>> watchAll({
    ProfilesSort sort = ProfilesSort.lastUpdate,
    SortMode sortMode = SortMode.ascending,
  }) {
    return _profileDataSource
        .watchAll(sort: sort, sortMode: sortMode)
        .map((event) => event.map((e) => e.toEntity()).toList())
        .handleExceptions(ProfileUnexpectedFailure.new);
  }

  @override
  TaskEither<ProfileFailure, Unit> upsertRemote(String url, {UserOverride? userOverride, CancelToken? cancelToken}) =>
      TaskEither.tryCatch(() async {
        var profEntity = await _profileDataSource.getByUrl(url).then((profEntry) => profEntry?.toEntity());
        final id = profEntity?.id ?? const Uuid().v4();
        final file = _profilePathResolver.file(id);
        final tempFile = _profilePathResolver.tempFile(id);
        try {
          final isUpdate = profEntity != null && profEntity is RemoteProfileEntity;
          late final ProfileEntriesCompanion parsedProfile;
          if (isUpdate) {
            var remoteProfile = profEntity as RemoteProfileEntity;
            if (userOverride != null) {
              remoteProfile = remoteProfile.copyWith(userOverride: userOverride);
            }
            parsedProfile = (await _profileParser
                    .updateRemote(rp: remoteProfile, tempFilePath: tempFile.path, cancelToken: cancelToken)
                    .run())
                .getOrElse((l) => throw l);
          } else {
            parsedProfile = (await _profileParser
                    .addRemote(
                      id: id,
                      url: url,
                      tempFilePath: tempFile.path,
                      userOverride: userOverride,
                      cancelToken: cancelToken,
                    )
                    .run())
                .getOrElse((l) => throw l);
          }
          (await validateConfig(file.path, tempFile.path, parsedProfile.profileOverride.value, false).run()).getOrElse(
            (l) => throw l,
          );
          if (isUpdate) {
            await _profileDataSource.edit(id, parsedProfile);
          } else {
            await _profileDataSource.insert(parsedProfile);
          }
          return unit;
        } finally {
          if (tempFile.existsSync()) tempFile.deleteSync();
        }
      }, (err, st) => err is ProfileFailure ? err : ProfileFailure.unexpected(err, st));

  @override
  TaskEither<ProfileFailure, Unit> addLocal(String content, {UserOverride? userOverride}) =>
      TaskEither.tryCatch(() async {
        final id = const Uuid().v4();
        final file = _profilePathResolver.file(id);
        final tempFile = _profilePathResolver.tempFile(id);
        try {
          await tempFile.writeAsString(content);
          final task = _profileParser
              .addLocal(id: id, content: content, tempFilePath: tempFile.path, userOverride: userOverride)
              .flatMap(
                (profEntity) =>
                    validateConfig(file.path, tempFile.path, profEntity.profileOverride.value, false).flatMap(
                      (unit) => TaskEither.tryCatch(() async {
                        await _profileDataSource.insert(profEntity);
                        return unit;
                      }, ProfileFailure.unexpected),
                    ),
              );
          return (await task.run()).getOrElse((l) => throw l);
        } finally {
          if (tempFile.existsSync()) tempFile.deleteSync();
        }
      }, ProfileFailure.unexpected);

  @override
  TaskEither<ProfileFailure, Unit> offlineUpdate(ProfileEntity profile, String nContent) =>
      TaskEither.tryCatch(() async {
        final oProfile = await _profileDataSource.getById(profile.id).then((profEntry) => profEntry?.toEntity());
        if (oProfile == null || oProfile.runtimeType != profile.runtimeType) throw const ProfileFailure.notFound();
        if (profile.userOverride == null) loggy.warning('Updaing profile content with "userOverride" == null');
        final id = oProfile.id;
        final file = _profilePathResolver.file(id);
        final tempFile = _profilePathResolver.tempFile(id);
        try {
          await tempFile.writeAsString(nContent);
          final parsedProfile = _profileParser
              .offlineUpdate(profile: oProfile.copyWith(userOverride: profile.userOverride), tempFilePath: tempFile.path)
              .getOrElse((l) => throw l);
          (await validateConfig(file.path, tempFile.path, parsedProfile.profileOverride.value, false).run()).getOrElse(
            (l) => throw l,
          );
          await _profileDataSource.edit(id, parsedProfile);
          return unit;
        } finally {
          if (tempFile.existsSync()) tempFile.deleteSync();
        }
      }, (err, st) => err is ProfileFailure ? err : ProfileFailure.unexpected(err, st));

  @override
  TaskEither<ProfileFailure, Unit> validateConfig(String path, String tempPath, String? profileOverride, bool debug) =>
      TaskEither.tryCatch(() async {
        await _sanitizeAwgSystemForWindows(tempPath);
        return unit;
      }, ProfileFailure.unexpected)
          .flatMap((_) {
            return TaskEither.fromEither(
              _configOptionRepo
                  .fullOptionsOverrided(profileOverride)
                  .mapLeft((configOptionFailure) => ProfileFailure.invalidConfig(null, configOptionFailure)),
            );
          })
          .flatMap(
            (overridedOptions) => _singbox
                .changeOptions(overridedOptions)
                .mapLeft(ProfileFailure.invalidConfig)
                .flatMap(
                  (_) => _singbox.validateConfigByPath(path, tempPath, debug).mapLeft(ProfileFailure.invalidConfig),
                ),
          );

  @override
  TaskEither<ProfileFailure, String> generateConfig(String id) => TaskEither.fromEither(
    Either.tryCatch(() => _profilePathResolver.file(id), ProfileFailure.unexpected),
  ).flatMap((configFile) => _singbox.generateFullConfigByPath(configFile.path).mapLeft(ProfileFailure.unexpected));

  @override
  TaskEither<ProfileFailure, String> getRawConfig(String id) {
    return TaskEither.fromEither(
      Either.tryCatch(() => _profilePathResolver.file(id), ProfileFailure.unexpected),
    ).flatMap((configFile) => TaskEither.tryCatch(() => configFile.readAsString(), ProfileFailure.unexpected));
  }
}
