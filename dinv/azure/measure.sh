#!/bin/bash
# measure sets up a bunch of measuring clients
# measure.sh words.txt [server ip:port] runtime clients etcdcdlLoc
#ex ./measure.sh /
RATE=1000
OUTPUT=latency.txt
i=0
CLIENTS=$4
echo "" > $OUTPUT


self=$$
(
    sleep $3;
    kill -9 $self;
) &

for (( i=0; i<CLIENTS; i++ ))
do
    ./client.sh $1 $2 $OUTPUT $5 &
done

