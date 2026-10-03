#!/usr/bin/env python3
"""反向验证 su_run：证明它不是「永远返回 PASS 的假检查」。

构造两个必然 FAIL 的场景，确认 su_run 能真实检出：
  A. 让 su_run 读一个**内容不符**的文件（模拟版本号错误）
  B. 让 su_run 读一个**不存在的文件**（模拟文件缺失）
若两者都能返回非预期值（而非空串或固定值），说明检查是有效的。
"""
import json
import os
import shlex
import time

import paramiko

# 仓库根定位：从本文件位置向上两级（scripts/ -> 仓库根）
# 不硬编码个人工作区绝对路径 —— 换机器/换人 clone 后同样能跑。
BASE = os.path.abspath(os.path.join(os.path.dirname(os.path.abspath(__file__)), ".."))
CFG = os.path.join(BASE, ".workbuddy", "ssh_config.json")
cfg = json.load(open(CFG, encoding="utf-8"))
ROOT_PW = cfg["users"]["root"]

cli = paramiko.SSHClient()
cli.set_missing_host_key_policy(paramiko.AutoAddPolicy())
cli.connect(cfg["host"], port=cfg.get("port", 22), username="warlocks",
            password=cfg["users"]["warlocks"], timeout=15)


def su_run(cmd, timeout=60):
    """与修复后的主脚本完全一致的实现（逐字复制，避免测的不是同一个东西）。"""
    chan = cli.get_transport().open_session()
    chan.get_pty()
    chan.exec_command("su -c " + shlex.quote(cmd))
    buf = b""
    sent = False
    deadline = time.time() + timeout
    while time.time() < deadline:
        if chan.recv_ready():
            chunk = chan.recv(65536)
            if not chunk:
                break
            buf += chunk
            if not sent and (b"assword" in buf or "密码".encode() in buf):
                chan.send(ROOT_PW + "\n")
                sent = True
        elif chan.exit_status_ready():
            break
        else:
            time.sleep(0.2)
    text = buf.decode("utf-8", "replace")
    lines = [l.strip() for l in text.splitlines()
             if l.strip() and "密码" not in l and "Password:" not in l]
    err_markers = ("没有那个文件", "权限不够", "Is a directory", "No such file",
                   "cannot ", "cannot open", "not found")
    lines = [l for l in lines if not any(m in l for m in err_markers)]
    return lines[-1] if lines else ""


print("=== 1. 正确场景（应返回 1.9.3）===")
r = su_run("cat /home/warlocks/pano-album/VERSION")
print(f"  返回={r!r}  期望='1.9.3'  判定={'PASS' if r == '1.9.3' else 'FAIL'}")

print()
print("=== 2. 反向 A：内容不符（应检出 FAIL）===")
r = su_run("echo 9.9.9")
print(f"  返回={r!r}  期望='9.9.9'（模拟版本号错）  "
      f"判定={'能检出差异 PASS' if r == '9.9.9' and r != '1.9.3' else '假检查 FAIL'}")

print()
print("=== 3. 反向 B：文件不存在（应返回空串而非误报内容）===")
r = su_run("cat /home/warlocks/pano-album/__NO_SUCH_FILE__")
print(f"  返回={r!r}  期望=''（空）  "
      f"判定={'能检出缺失 PASS' if r == '' else '假检查 FAIL: ' + r!r}")

print()
print("=== 4. 反向 C：grep 无命中应返回 0 而非 1 ===")
r = su_run("grep -c '这个字符串绝对不存在XYZ' /home/warlocks/pano-album/src/frontend/src/manual/content-basics.js")
print(f"  返回={r!r}  期望='0'  判定={'PASS' if r == '0' else 'FAIL'}")

print()
print("=== 5. 反向 D：带空格/中文参数不���被吞（shlex.quote 有效性）===")
r = su_run("grep -c 'Range 流式' /home/warlocks/pano-album/src/frontend/src/manual/content-basics.js")
print(f"  返回={r!r}  期望='1'  判定={'PASS' if r == '1' else 'FAIL'}")

cli.close()
