import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:hiddify/features/profile/details/profile_details_notifier.dart';

void main() {
  group('ProfileDetailsNotifier.prepareEditorContent', () {
    test('preserves full MASQUE profile JSON for raw editing', () {
      const content = r'''
{
  "log": {
    "level": "debug"
  },
  "dns": {
    "servers": [
      {
        "tag": "dns-direct",
        "address": "1.1.1.1"
      }
    ]
  },
  "endpoints": [
    {
      "type": "warp_masque",
      "tag": "warp-masque",
      "http_layer": "h2",
      "http_layer_fallback": false,
      "template_ip": "https://cloudflareaccess.com/cdn-cgi/masque",
      "server_token": "test-server-token",
      "profile": {
        "id": "test-device",
        "auth_token": "test-auth-token",
        "masque_ecdsa_private_key": "test-ecdsa-key"
      }
    }
  ]
}
''';

      final prepared = ProfileDetailsNotifier.prepareEditorContent(content);
      final decoded = jsonDecode(prepared) as Map<String, dynamic>;
      final endpoint = (decoded['endpoints'] as List).single as Map<String, dynamic>;
      final profile = endpoint['profile'] as Map<String, dynamic>;

      expect(decoded['dns'], isNotNull);
      expect(endpoint['type'], 'warp_masque');
      expect(endpoint['http_layer'], 'h2');
      expect(endpoint['http_layer_fallback'], false);
      expect(endpoint['template_ip'], contains('cloudflareaccess.com'));
      expect(endpoint['server_token'], 'test-server-token');
      expect(profile['auth_token'], 'test-auth-token');
      expect(profile['masque_ecdsa_private_key'], 'test-ecdsa-key');
    });

    test('keeps legacy projection for non-MASQUE generated configs', () {
      const content = r'''
{
  "dns": {
    "servers": [
      "1.1.1.1"
    ]
  },
  "outbounds": [
    {
      "type": "selector",
      "tag": "select"
    },
    {
      "type": "vless",
      "tag": "proxy"
    }
  ],
  "endpoints": [],
  "inbounds": []
}
''';

      final prepared = ProfileDetailsNotifier.prepareEditorContent(content);
      final decoded = jsonDecode(prepared) as Map<String, dynamic>;

      expect(decoded.containsKey('dns'), false);
      expect(decoded['outbounds'], [
        {'type': 'vless', 'tag': 'proxy'},
      ]);
    });
  });
}
