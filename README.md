# telegraf-lxd-stats
LXD stats telegraf plugin

1.
Put telegraf-lxd-stats binary to e.g. /usr/local/sbin/telegraf-lxd-stats

2.
Add to telegraf configuration file (e.g. /etc/telegraf/telegraf.conf):
```
[[inputs.exec]]
  command = "sudo /usr/local/sbin/telegraf-lxd-stats"
  data_format = "influx"
```
3.
Add to /etc/sudoers file (telegraf daemon user by default):
```
telegraf ALL = NOPASSWD: /usr/local/sbin/telegraf-lxd-stats
```
