#!/bin/bash
while true ; do 
start=`ifconfig lo | grep "TX packets" | awk '{print $2 }' | cut -d: -f2`
sleep 1
end=`ifconfig lo | grep "TX packets" | awk '{print $2 }' | cut -d: -f2`
let pktsec=$end-$start
echo "$pktsec / second"
done
