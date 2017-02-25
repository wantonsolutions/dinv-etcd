#!/bin/bash

TESTS=5
NAME="SL-Sample-10"
#PULL
#./cluster.sh -p

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
    ./cluster.sh -r 400 "$NAME-high"
done
