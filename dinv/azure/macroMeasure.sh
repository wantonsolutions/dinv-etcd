#!/bin/bash

TESTS=$1
NAME="SL-Sample-0"
#PULL
./cluster.sh -p

for (( i=0; i <TESTS; i ++))
do
    ./cluster.sh -r 4 "$NAME-low"
done

for (( i=0; i <TESTS; i ++))
do
    ./cluster.sh -r 40 "$NAME-med"
done

for (( i=0; i <TESTS; i ++))
do
    ./cluster.sh -r 40 "$NAME-high"
done
