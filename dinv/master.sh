#!/bin/bash
./clean.sh
sudo -E go install ../

if [ $1 == -b ]; then
    exit
fi

rm *.time
rm *.txt
./modcluster.sh 3 &
sleep 3
./clientblast.sh kahn.in &
sleep 15
killall etcd
killall clientblast.sh
sleep 3

read ktime

cat *.time | grep time:
ls -lrt *.txt | nawk '{print $5}' | awk '{total = total + $1}END{print total}'
time dinv -l -plan=SCM -json -name=fruits -shiviz *d.txt *g.txt

