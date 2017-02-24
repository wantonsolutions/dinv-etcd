#client makes requets to a raft webserver and keeps track of the latency of each request

#client.sh intput.txt [server ip:port]
for word in $(<$1)
do
    latency=`time ETCDCTL_API=3 ../bin/etcdctl --endpoints=$2 put $i "$word"`
    echo "$latency," >> latency.csv

done
