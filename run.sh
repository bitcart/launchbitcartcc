#!/bin/bash

cat <<EOM >/root/run.py
import os
import subprocess
import sys
import time

hostname = "[HOSTNAME]"
network = "[NETWORK]"
email = "[EMAIL]"
repository = "[REPOSITORY]"
branch = "[BRANCH]"
coinstr = "[COINS]"
lightning = "[LIGHTNING]"

reverseproxy = "nginx-https"
coinmap = ["btc", "bch", "ltc", "gzro", "bsty"]

# clone bitcart-docker
if not os.path.exists("/root/bitcart-docker"):
    subprocess.call(["git", "clone", repository, "bitcart-docker"], cwd="/root/")
    subprocess.call(["git", "checkout", branch], cwd="/root/bitcart-docker")

env = os.environ.copy()
coins = coinstr.split(",")

for coin in coins:
    if coin in coinmap:
        env["{}_NETWORK".format(coin)] = network
        env["{}_LIGHTNING".format(coin)] = lightning

env["BITCART_CRYPTOS"] = coinstr
env["BITCART_REVERSEPROXY"] = reverseproxy
env["BITCART_HOST"] = hostname
env["BITCART_LETSENCRYPT_EMAIL"] = email

for i in range(5):
    popen = subprocess.Popen(
        ["bash", "-c", ". ./setup.sh"],
        stdout=subprocess.PIPE,
        stderr=subprocess.STDOUT,
        env=env,
        cwd="/root/bitcart-docker",
    )
    had_error = False
    for line in popen.stdout:
        sys.stdout.buffer.write(b"[bitcart-setup] ")
        sys.stdout.buffer.write(line)
        if (
            b"Could not resolve host:" in line
            or b"docker-compose: command not found" in line
        ):
            had_error = True
    popen.stdout.close()
    return_code = popen.wait()
    if return_code == 0 and not had_error:
        subprocess.Popen(
            ["bash", "-c", ". ./start.sh"],
            stdout=subprocess.PIPE,
            stderr=subprocess.STDOUT,
            env=env,
            cwd="/root/bitcart-docker",
        ).wait()
        break
    else:
        print("launcher: bitcart-setup script had error, retrying in 10 seconds")
        time.sleep(10)
        continue
EOM

# for now this should be enough time to attach volumes
# later on we may need to do something more robust
sleep 20
/usr/bin/python3 /root/run.py

[ -x "$(command -v /etc/init.d/sshd)" ] && nohup /etc/init.d/sshd restart &
[ -x "$(command -v /etc/init.d/ssh)" ] && nohup /etc/init.d/ssh restart &
