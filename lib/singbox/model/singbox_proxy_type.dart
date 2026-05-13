enum ProxyType {
  direct("Direct"),
  block("Block"),
  dns("DNS"),
  socks("SOCKS"),
  http("HTTP"),
  shadowsocks("Shadowsocks"),
  vmess("VMess"),
  trojan("Trojan"),
  naive("Naive"),
  wireguard("WireGuard"),
  awg("AWG"),
  hysteria("Hysteria"),
  tor("Tor"),
  ssh("SSH"),
  shadowtls("ShadowTLS"),
  shadowsocksr("ShadowsocksR"),
  vless("VLESS"),
  tuic("TUIC"),
  hysteria2("Hysteria2"),
  mieru("Mieru"),

  selector("Selector"),
  urltest("URLTest"),
  balancer("Balancer"),
  warp("Warp"),
  masque("MASQUE"),
  warpMasque("WARP MASQUE"),

  xvless("xVLESS"),
  xvmess("xVMess"),
  xtrojan("xTrojan"),
  xfreedom("xFragment"),
  xshadowsocks("xShadowsocks"),
  xsocks("xSocks"),
  invalid("Invalid"),
  unknown("Unknown");

  const ProxyType(this.label);

  final String label;

  String get key => switch (this) {
    warpMasque => 'warp_masque',
    _ => name,
  };

  static List<ProxyType> groupValues = [selector, urltest, balancer];

  bool get isGroup => ProxyType.groupValues.contains(this);
  static final Map<String, ProxyType> _keyMap = {
    for (final value in ProxyType.values) value.name.toLowerCase(): value,
    for (final value in ProxyType.values) value.key.toLowerCase(): value,
  };
  static ProxyType fromJson(dynamic type) => _keyMap[(type as String?)?.toLowerCase()] ?? ProxyType.unknown;
}
