"""One-shot: append local my_server pubkey to root@$SSH_INSTALL_HOST authorized_keys (password once)."""
import os
import subprocess
import sys

import paramiko


def main() -> None:
    pw = os.environ.get("RDPW", "")
    host = os.environ.get("SSH_INSTALL_HOST", "")
    if not pw or not host:
        print("need RDPW and SSH_INSTALL_HOST", file=sys.stderr)
        sys.exit(2)
    key = subprocess.check_output(
        ["ssh-keygen", "-y", "-f", r"C:\Users\qwerty\.ssh\my_server"],
        text=True,
    ).strip()
    if not key.startswith("ssh-"):
        print("bad pubkey", file=sys.stderr)
        sys.exit(2)
    line = key + "\n"
    fp = key.split()[1]
    c = paramiko.SSHClient()
    c.set_missing_host_key_policy(paramiko.AutoAddPolicy())
    c.connect(host, username="root", password=pw, timeout=30, allow_agent=False, look_for_keys=False)
    stdin, stdout, stderr = c.exec_command("mkdir -p /root/.ssh && chmod 700 /root/.ssh")
    stdout.channel.recv_exit_status()
    sftp = c.open_sftp()
    try:
        with sftp.open("/root/.ssh/authorized_keys", "r") as f:
            data = f.read().decode(errors="replace")
    except OSError:
        data = ""
    if fp in data:
        print("key already present")
    else:
        with sftp.open("/root/.ssh/authorized_keys", "w") as f:
            f.write(data + line)
        print("key appended")
    stdin, stdout, stderr = c.exec_command("chmod 600 /root/.ssh/authorized_keys")
    stdout.channel.recv_exit_status()
    c.close()


if __name__ == "__main__":
    main()
