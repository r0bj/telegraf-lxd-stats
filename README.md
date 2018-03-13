# telegraf-lxd-stats
LXD stats telegraf plugin

1. Compilation
```go
go get github.com/r0bj/telegraf-lxd-stats
cd $GOPATH/src/github.com/r0bj/telegraf-lxd-stats
go build telegraf-lxd-stats.go
cp telegraf-lxd-stats /usr/local/sbin/
```

2. Configuration
Add to telegraf configuration file (e.g. /etc/telegraf/telegraf.d/lxdstats.conf):
```
[[inputs.exec]]
  command = "sudo /usr/local/sbin/telegraf-lxd-stats"
  data_format = "influx"
```

Add to /etc/sudoers.d/lxdstats file (telegraf daemon user by default):
```
telegraf ALL = NOPASSWD: /usr/local/sbin/telegraf-lxd-stats
```

Example plugin output for three containers:
```
lxcstats,lxc_host=vpn01 mem_limit=8370376704,blkio_writes=392,mem_usage=90316800,cpu_time_percpu=79701204098.166672,bytes_sent=808700591,blkio_read_bytes=375410688,blkio_write_bytes=57102336,cpu_time=478207224589,blkio_reads=10884,mem_usage_perc=1.079005,bytes_recv=1581083280
lxcstats,lxc_host=git01 bytes_sent=23843019,bytes_recv=32276618,blkio_writes=448,blkio_read_bytes=523169792,mem_usage_perc=1.316827,cpu_time_percpu=69940089393.000000,mem_limit=8370376704,blkio_reads=12794,cpu_time=419640536358,blkio_write_bytes=112340992,mem_usage=110223360
lxcstats,lxc_host=influxdb01 mem_limit=8370376704,blkio_reads=14578,blkio_writes=32357,cpu_time=1745857682539,blkio_write_bytes=373084160,mem_usage_perc=3.338359,bytes_sent=255192001,bytes_recv=131132657,blkio_read_bytes=528297984,mem_usage=279433216,cpu_time_percpu=290976280423.166687
```

Example grafana graphs based on plugin:
![ScreenShot](/screenshots/ss1.png)
![ScreenShot](/screenshots/ss2.png)
![ScreenShot](/screenshots/ss3.png)

An example Grafana dashboard is included with the source.
