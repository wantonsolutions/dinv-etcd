#!/bin/bash

TESTS=$1
for (( i=0; i <TESTS; i ++))
do
    ./cluster.sh -r
done
