#!/bin/bash

RATE=1000
OUTPUT=count.txt
i=0
t=$3
echo "" > $OUTPUT


self=$$
(
    sleep $4;
    kill -9 $self;
) &

for word in $(<$1)
do
    ETCDCTL_API=3 ../bin/etcdctl --endpoints=$2 put $i "$word" &
    echo $i
    sleep $t
    i=$((i+1))
done >> $OUTPUT


#i=0
#for word in $(<$1)
#do
#    ETCDCTL_API=3 ../bin/etcdctl --endpoints=localhost:2379 get $i
#    i=$((i+1))
#done

