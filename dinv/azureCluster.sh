#!/bin/bash

#stewart-test-1
GLOBALS1=10.0.1.4
LOCALS1=52.228.27.112
#stewart-test-2
LOCALS2=52.228.32.101
GLOBALS2=10.0.1.5
#stewart
LOCALS3=13.64.239.61
GLOBALS3=10.0.0.4
:'
#stewart-test-1
LOCALS1=10.0.1.4
GLOBALS1=52.228.27.112
#stewart-test-2
GLOBALS2=52.228.32.101
LOCALS2=10.0.1.5
#stewart
GLOBALS3=13.64.239.61
LOCALS3=10.0.0.4
'
LOCAL=127.0.0.1

HOME=/home/stewart
DINV=$HOME/go/src/bitbucket.org/bestchai/dinv
ETCD=$HOME/go/src/github.com/coreos/etcd
ETCDCMD=$HOME/go/src/github.com/coreos/etcd/bin/etcd

TEXT=kahn.in

#Example execute ssh 
#ssh stewart@13.64.239.61 -x "mkdir test"

#Example execute scp
#scp stewart@13.64.239.61:/home/stewart/azureinstall.sh astest

ssh stewart@$LOCALS1 -x "$ETCDCMD --name infra0 --initial-advertise-peer-urls http://$GLOBALS1:2380 \
  --listen-peer-urls http://$GLOBALS1:2380 \
  --listen-client-urls http://$GLOBALS1:2379,http://$LOCAL:2379 \
  --advertise-client-urls http://$GLOBALS1:2379 \
  --initial-cluster-token etcd-cluster-1 \
  --initial-cluster infra0=http://$GLOBALS1:2380,infra1=http://$GLOBALS2:2380,infra2=http://$GLOBALS3:2380 \
  --initial-cluster-state new " &

ssh stewart@$LOCALS2 -x "$ETCDCMD --name infra1 --initial-advertise-peer-urls http://$GLOBALS2:2380 \
  --listen-peer-urls http://$GLOBALS2:2380 \
  --listen-client-urls http://$GLOBALS2:2379,http://$LOCAL:2379 \
  --advertise-client-urls http://$GLOBALS2:2379 \
  --initial-cluster-token etcd-cluster-1 \
  --initial-cluster infra0=http://$GLOBALS1:2380,infra1=http://$GLOBALS2:2380,infra2=http://$GLOBALS3:2380 \
  --initial-cluster-state new " &

ssh stewart@$LOCALS3 -x "$ETCDCMD --name infra2 --initial-advertise-peer-urls http://$GLOBALS3:2380 \
  --listen-peer-urls http://$GLOBALS3:2380 \
  --listen-client-urls http://$GLOBALS3:2379,http://$LOCAL:2379 \
  --advertise-client-urls http://$GLOBALS3:2379 \
  --initial-cluster-token etcd-cluster-1 \
  --initial-cluster infra0=http://$GLOBALS1:2380,infra1=http://$GLOBALS2:2380,infra2=http://$GLOBALS3:2380 \
  --initial-cluster-state new" &

ssh stewart@$LOCALS3 -x "./azureBlast.sh $TEXT $LOCALS1"

sleep 5
