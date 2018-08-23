#!/bin/sh
check_remote_ssh() {
  pid=`ps -ef | grep -v grep | grep "remote_ssh"  |grep -v check|awk '{print $2}'`
  if [ ! -n "$pid" ]; then
     current_dir=$(cd `dirname $0`; pwd)
     #${current_dir}/remote_ssh -L ${current_dir}/remote_log -D 
	 nohup ${current_dir}/remote_ssh -L ${current_dir}/remote_log &
     echo "nohup"
  else
    return 0
  fi
  return 0
}

while true
do
  check_remote_ssh
done
