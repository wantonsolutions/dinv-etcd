#!/bin/bash
sudo -E go install ../
rm *.time
rm *.txt
./modcluster.sh 3 &
sleep 5 #was 5
./clientblast.sh dec.in &
sleep 5
killall etcd
killall clientblast.sh
sleep 3

cat *.time | grep time:
ls -lrt *.txt | nawk '{print $5}' | awk '{total = total + $1}END{print total}'
#time dinv -l -plan=SCM -json -name=fruits *d.txt *g.txt

