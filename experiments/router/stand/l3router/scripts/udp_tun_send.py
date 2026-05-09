import socket
import sys
import time

N = int(sys.argv[1])
HOST = sys.argv[2]
PORT = int(sys.argv[3])
CHUNK = int(sys.argv[4]) if len(sys.argv) > 4 else 8192
if CHUNK < 1 or CHUNK > 65507:
    CHUNK = 8192
PAUSE = float(sys.argv[5]) if len(sys.argv) > 5 else 0.0
if PAUSE < 0:
    PAUSE = 0.0
RATE_BPS = int(sys.argv[6]) if len(sys.argv) > 6 else 0
if RATE_BPS < 0:
    RATE_BPS = 0
SNDBUF = int(sys.argv[7]) if len(sys.argv) > 7 else 0

sock = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
if SNDBUF > 0:
    sock.setsockopt(socket.SOL_SOCKET, socket.SO_SNDBUF, SNDBUF)

payload = b"\x00" * CHUNK
sent = 0
start = time.monotonic()
while sent < N:
    n = min(CHUNK, N - sent)
    sock.sendto(payload[:n], (HOST, PORT))
    sent += n
    if RATE_BPS > 0:
        target_elapsed = sent / float(RATE_BPS)
        sleep_for = target_elapsed - (time.monotonic() - start)
        if sleep_for > 0:
            time.sleep(sleep_for)
    elif PAUSE > 0:
        time.sleep(PAUSE)
sock.close()
