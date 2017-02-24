#!/bin/bash

#Author Stewart Grant
#Feb 2016
#FSE deadline push

#This script mannages running etcd clusters on azure. The clusters are
#run in vm's which are ssh'd into. The names of all of the VMs are
#abbrivated below #Etcd is launched along with a client that makes puts
#and gets. After a predetermined ammount of time the cluster is killed
#and the logs are retrieved via scp.


#VM's their public and private IP's
#stewart-test-1
ST1G=52.228.27.112
ST1P=10.0.1.4
#stewart-test-2
ST2G=52.228.32.101
ST2P=10.0.1.5
#stewart
S1G=13.64.239.61
S1P=10.0.0.4
#stewart2
S2G=13.64.247.122
S2P=10.0.0.5
#stewart3
S3G=13.64.242.139
S3P=10.0.0.6


#map a subset of the VM's to the current cluster
GLOBALS1=$S1G
LOCALS1=$S1P
GLOBALS2=$S2G
LOCALS2=$S2P
GLOBALS3=$S3G
LOCALS3=$S3P

#GLOBAL information true of all VMs
HOMEA=/home/stewart
DINV=$HOMEA/go/src/bitbucket.org/bestchai/dinv
ETCD=$HOMEA/go/src/github.com/coreos/etcd
ETCDCMD=$HOMEA/go/src/github.com/coreos/etcd/bin/etcd
ETCDCTL=$HOMEA/go/src/github.com/coreos/etcd/bin/etcdctl
AZURENODE=/dinv/azure/node.sh
#different clients
#CLIENT=/dinv/azure/blast.sh
CLIENT=/dinv/azure/client.sh

#LOCAL
DINVDIR=/home/stewartgrant/go/src/bitbucket.org/bestchai/dinv

#MEASUREMENT INFO
MEASURE=true


TEXT=$ETCD/dinv/kahn.in
#TEXT=/usr/share/dict/words

USAGE="USAGE\n-k kill all nodes in the cluster\n-p pull from etcd-dinv repo\n-l logmerger\n-d Daikon\n-c clean"

function onall {
    ssh stewart@$GLOBALS1  -x $1  &
    ssh stewart@$GLOBALS2  -x $1  &
    ssh stewart@$GLOBALS3  -x $1  &
    #ssh stewart@$ST1G -x $1 &
    #ssh stewart@$ST2G -x $1 &
    #ssh stewart@$SG -x $1 &
    #ssh stewart@$S2G -x $1 &
    #ssh stewart@$S3G -x $1 &
}

#kill all the nodes
if [ "$1" == "-k" ];then
    echo kill
    onall "killall etcd; killall blast.sh"
    exit
fi

#have all the nodes pull new code
if [ "$1" == "-p" ];then
    echo clean
    $DINVDIR/examples/lib.sh clean
    echo push
    cd ../../
    git add --all
    git commit -m "updating raft for peers"
    git push
    cd dinv/azure
    echo pull
    onall "cd $ETCD && git pull && ./build ; cd $DINV && hg pull"
    exit
fi

#run logmerger
if [ "$1" == "-l" ];then
    sudo -E go install ../../../../../bitbucket.org/bestchai/dinv
    $DINVDIR/examples/lib.sh runLogMerger "-plan=SCM -shiviz"
    exit
fi

#run daikon
if [ "$1" == "-d" ];then
    $DINVDIR/examples/lib.sh runDaikon
    exit
fi

#clean
if [ "$1" == "-c" ];then
    echo clean
    $DINVDIR/examples/lib.sh clean
    exit
fi


if [ "$1" == "-r" ];then
    echo run

    #Example execute ssh 
    #ssh stewart@13.64.239.61 -x "mkdir test"

    #Example execute scp
    #scp stewart@13.64.239.61:/home/stewart/azureinstall.sh astest

    #LOCAL CLUSTER
    CLUSTER="infra0=http://$LOCALS1:2380,infra1=http://$LOCALS2:2380,infra2=http://$LOCALS3:2380"
    ASSERT="$LOCALS1:12000,$LOCALS2:12000,$LOCALS3:12000"
    ssh stewart@$GLOBALS1 -x "$ETCD$AZURENODE 0 $GLOBALS1 $LOCALS1 $CLUSTER $ASSERT" &
    ssh stewart@$GLOBALS2 -x "$ETCD$AZURENODE 1 $GLOBALS2 $LOCALS2 $CLUSTER $ASSERT" &
    ssh stewart@$GLOBALS3 -x "$ETCD$AZURENODE 2 $GLOBALS3 $LOCALS3 $CLUSTER $ASSERT" &

    #GLOBAL CLUSTER
    #CLUSTER="infra0=http://$GLOBALS1:2380,infra1=http://$GLOBALS2:2380,infra2=http://$GLOBALS3:2380"
    #ASSERT="$GLOBALS1:12000,$GLOBALS2:12000,$GLOBALS3:12000"
    #ssh stewart@$GLOBALS1 -x "$ETCD$AZURENODE 0 $GLOBALS1 $GLOBALS1 $CLUSTER $ASSERT" &
    #ssh stewart@$GLOBALS2 -x "$ETCD$AZURENODE 1 $GLOBALS2 $GLOBALS2 $CLUSTER $ASSERT" &
    #ssh stewart@$GLOBALS3 -x "$ETCD$AZURENODE 2 $GLOBALS3 $GLOBALS3 $CLUSTER $ASSERT" &
    sleep 5

    if [ "$MEASURE" = false ] ; then
        #run the client on on the same node it's sending to
        ssh stewart@$GLOBALS1 -x "echo $ETCD/dinv/azure/client.sh $TEXT $LOCALS1 && $ETCD/dinv/azure/client.sh $TEXT $LOCALS1 $ETCDCTL" &
        #kill allthe hosts
        sleep 10
        echo kill
        onall "killall etcd"
        onall "killall blast"
    fi

    #run the client on a node seperate from the one receving
    #ssh stewart@$GLOBALS1 -x "$ETCD$CLIENT $TEXT $LOCALSS2" &

    if [ "$MEASURE" = true ] ; then

        EXP="control"
        RUNTIME=10
        CLIENTS=1
        #run the client locally
        #./measure.sh /usr/share/dict/words $GLOBALS1:2379 $RUNTIME
        echo "STARTING CLIENT"
        ssh stewart@$GLOBALS1 -x "echo $ETCD$CLIENT $TEXT $LOCALS1 && $ETCD$CLIENT $TEXT $LOCALS1 $ETCDCTL"
        #./client.sh /usr/share/dict/words $GLOBALS1:2379
        TP=`grep -E '[0-9]' count.txt | wc -l | cut -f1`
        echo "$EXP,$RATE,$LENGTH,$TP" >> measurements.txt
        #kill allthe hosts
        echo kill
        onall "killall etcd"
        onall "killall blast"
    fi

    #wait for the test to run

    scp stewart@$GLOBALS1:/home/stewart/*.txt ./
    scp stewart@$GLOBALS2:/home/stewart/*.txt ./
    scp stewart@$GLOBALS3:/home/stewart/*.txt ./
    echo DONE!
    exit
fi

echo -e $USAGE
