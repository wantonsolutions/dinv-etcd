#client makes requets to a raft webserver and keeps track of the latency of each request

#client.sh intput.txt [server ip:port]
LatOut=latency.txt
echo "" > $LatOut
for word in $(<$1)
do
    ETCDCTL_API=3 /usr/bin/time -f "%E" $3 --endpoints=$2 put $i "$word"
    echo "$latency" >> $LatOut
    #echo "making request"
    #ETCDCTL_API=3 $3 --endpoints=$2 put $i "$word"

done
