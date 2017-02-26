#!/bin/bash

#Launching Script for Etcd cluster

PEERS=$1

CLUSTERSTRING=""
DINV_ASSERT_PEERS=""

#TERMINAL=gnome-terminal -x
TERMINAL=""

for i in $(seq 1 $PEERS)
do
    CLUSTERSTRING=$CLUSTERSTRING"infra"`expr $i - 1`"=http://127.0.0."$i":2380,"
    DINV_ASSERT_PEERS=$DINV_ASSERT_PEERS"127.0.0."$i":12000,"

done

echo $CLUSTERSTRING

fuser -k 2380/tcp
rm -r *[0-9].etcd
sudo -E go install ../

ASSERTTYPE="STRONGLEADER"
LEADER="true"
SAMPLE="10"
DINVBUG="true"
#export assert macros
export LEADER
export ASSERTTYPE
export SAMPLE
export DINVBUG

for i in $(seq 1 $PEERS)
do
    infra="infra"`expr $i - 1`
    #Setup assert names, each node is given an ip port 127.0.0.(id):12000
    DINV_HOSTNAME="NODE"$i
    DINV_ASSERT_LISTEN="127.0.0."$i":12000"
    #export each of the names before launching a node
    export DINV_HOSTNAME
    export DINV_ASSERT_PEERS
    export DINV_ASSERT_LISTEN
    #launch the nodes
    $TERMINAL etcd --name $infra --initial-advertise-peer-urls http://127.0.0.$i:2380 \
      --listen-peer-urls http://127.0.0.$i:2380 \
      --listen-client-urls http://127.0.0.$i:2379,http://127.0.0.$i:2379 \
      --advertise-client-urls http://127.0.0.$i:2379 \
      --initial-cluster-token etcd-cluster-1 \
      --initial-cluster $CLUSTERSTRING \
      --initial-cluster-state new &
done
