#!/bin/bash

echo "starting azure client"
echo "" > latency.txt
i=0
for word in $(<$1)
do

    a=$(($(date +%s%N)/1000000))
    ETCDCTL_API=3 $3 --endpoints=$2:2379 put $i "$word"
    b=$(($(date +%s%N)/1000000))
    lat=0
    let lat=b-a
    echo $lat >> latency.txt

#    ETCDCTL_API=3 ../bin/etcdctl --endpoints=localhost:2379 get $i
    i=$((i+1))
done


#i=0
#for word in $(<$1)
#do
#    ETCDCTL_API=3 $3 --endpoints=$2:2379 get $i
#    i=$((i+1))
#done
