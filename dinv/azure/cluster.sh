#!/bin/bash




#stewart-test-1
LOCALS1=10.0.1.4
GLOBALS1=52.228.27.112
#stewart-test-2
GLOBALS2=52.228.32.101
LOCALS2=10.0.1.5
#stewart
GLOBALS3=13.64.239.61
LOCALS3=10.0.0.4

HOME=/home/stewart
DINV=$HOME/go/src/bitbucket.org/bestchai/dinv
ETCD=$HOME/go/src/github.com/coreos/etcd
ETCDCMD=$HOME/go/src/github.com/coreos/etcd/bin/etcd
AZURENODE=/dinv/azure/node.sh

TEXT=kahn.in

function onall {
    ssh stewart@$GLOBALS1 -x $1
    ssh stewart@$GLOBALS2 -x $1
    ssh stewart@$GLOBALS3 -x $1
}

if [ "$1" == "-k" ];then
    echo kill
    onall "killall etcd"
    exit
fi

#Example execute ssh 
#ssh stewart@13.64.239.61 -x "mkdir test"

#Example execute scp
#scp stewart@13.64.239.61:/home/stewart/azureinstall.sh astest


CLUSTER="infra0=http://$LOCALS1:2380,infra1=http://$LOCALS2:2380,infra2=http://$LOCALS3:2380"
ASSERT="$LOCALS1:12000,$LOCALS2:12000,$LOCALS3:12000"



ssh stewart@$GLOBALS1 -x "$ETCD$AZURENODE 0 $GLOBALS1 $LOCALS1 $CLUSTER $ASSERT" &
ssh stewart@$GLOBALS2 -x "$ETCD$AZURENODE 1 $GLOBALS2 $LOCALS2 $CLUSTER $ASSERT" &
ssh stewart@$GLOBALS3 -x "$ETCD$AZURENODE 2 $GLOBALS3 $LOCALS3 $CLUSTER $ASSERT" &



