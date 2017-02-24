#!/bin/bash

RATE=10000
OUTPUT=count.txt
i=0
t=0.001
echo "" > $OUTPUT


self=$$
(
    sleep 5;
    kill -9 $self;
) &

for word in $(<$1)
do
    a=$(($(date +%s%N)/1000000))
    ETCDCTL_API=3  ../bin/etcdctl --endpoints=localhost:2379 put $i "$word"
    b=$(($(date +%s%N)/1000000))
    lat=0
    let lat=b-a
    echo $lat
    i=$((i+1))
    echo $i
    if [ "$i" -eq "100" ];then
        exit
    fi

    echo "done"
done >> $OUTPUT


#i=0
#for word in $(<$1)
#do
#    ETCDCTL_API=3 ../bin/etcdctl --endpoints=localhost:2379 get $i
#    i=$((i+1))
#done

