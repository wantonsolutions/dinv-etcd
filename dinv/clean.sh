#!/bin/bash

testDir=~/go/src/github.com/coreos/etcd/dinv
dinvDir=~/go/src/bitbucket.org/bestchai/dinv



$dinvDir/examples/lib.sh clean


function movelogs {
    cd $testDir
    shopt -s nullglob
    set -- *[gd].txt
    if [ "$#" -gt 0 ]
    then
        name=`date "+%m-%d-%y-%s"`
        mkdir old/$name
        mv *[gd].txt old/$name
        mv *.dtrace old/$name
        mv *.gz old/$name
        mv *.json old/$name
    fi
}

#movelogs
