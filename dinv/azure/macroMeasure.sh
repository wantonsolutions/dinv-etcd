#!/bin/bash

#cluster args
#arguments
#1 function
#2 clients
#3 name
#4 assertType
#5 leader
#6 sample

TESTS=1
NAME="SL-Sample-100"
#PULL
#./cluster.sh -p

for (( i=0; i <TESTS; i ++))
do
    ./cluster.sh -r 4 "$NAME-low" "STRONGLEADER" "FALSE" "1"
done

