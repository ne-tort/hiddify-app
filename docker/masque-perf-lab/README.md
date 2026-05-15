# MASQUE perf lab

**`AGENTS.md` §15** — эталон: `scripts/Benchmark-Masque.ps1` (по умолчанию **TUN** + iperf TCP; для **h3/h2** при **tun** — ещё короткий **iperf3 -u** как probe **CONNECT-UDP**).

```powershell
powershell -NoProfile -File scripts\Benchmark-Masque.ps1 -SkipBuild
powershell -NoProfile -File scripts\Benchmark-Masque.ps1 -SkipBuild -BenchVia all
```

Проверка TCP через TUN в контейнере:

```powershell
Docker Compose подставляет **`${...}`** из файла **`docker/masque-perf-lab/.env`** (рядом с **`docker-compose.remote.yml`**), если он есть:

- **`HIDDIFY_MASQUE_CONNECT_IP_DEBUG=1`** — расширенные логи gVisor/netstack на клиенте (см. `AGENTS.md` §15).
- **`MASQUE_QUIC_PACKET_CONN_POLICY=strict|permissive`** — контракт QUIC `PacketConn` для MASQUE (см. `transport/masque/transport.go`).

```env
HIDDIFY_MASQUE_CONNECT_IP_DEBUG=1
# MASQUE_QUIC_PACKET_CONN_POLICY=strict
```

