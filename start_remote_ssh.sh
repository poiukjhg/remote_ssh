#!/bin/sh
pid=""
pid=`ps -ef |grep -v grep|grep -v start|grep remote_ssh|awk '{print $2}'`
current_dir=$(cd `dirname $0`; pwd)
echo $pid
if [ ! -n "$pid" ]; then
    echo  "start check remote_ssh process"
    nohup sh ${current_dir}/check_remote_ssh.sh &
    exit
else
    echo "check remote_ssh is already exist"
    exit
fi
