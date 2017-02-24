#!/bin/bash

#input files
#dog.in the quick
#kahn.in kublakahn
#dec.in declaration
#in.in test*30000
#/usr/share/dict/words words

./clean.sh
sudo -E go install ../

if [ $1 == -b ]; then
    exit
fi

rm *.time
rm *.txt
./modcluster.sh 3 &
sleep 3
#./clientblast.sh /usr/share/dict/words &
./clientMeasure.sh /usr/share/dict/words &
sleep 20
killall etcd
killall clientMeasure.sh

cat *.time | grep time:
ls -lrt *.txt | nawk '{print $5}' | awk '{total = total + $1}END{print total}'
time dinv -l -plan=SCM -json -name=fruits -shiviz *d.txt *g.txt
./daikon.sh
