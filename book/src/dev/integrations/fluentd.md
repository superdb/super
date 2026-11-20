# Fluentd

TODO: Phil to update this.

The [Fluentd](https://www.fluentd.org/) open source data collector can be used
to push log data to a [SuperDB database](../../database/intro.md) in a continuous manner.
This allows for querying near-"live" event data to enable use cases such as
dashboarding and alerting in addition to creating a long-running historical
record for archiving and analytics.

This guide walks through two simple configurations of Fluentd with a SuperDB database
that can be used as reference for starting your own production configuration.
As it's a data source important to many in the SuperDB community, log data from
[Zeek](zeek/intro.md) is used in this guide. The approach shown can be
easily adapted to any log data source.

## Software

The examples were tested on an AWS EC2 `t2.large` instance running Ubuntu
Linux 24.04. At the time this article was written, the following versions
were used for the referenced software:

* [Fluentd v1.17.0](https://github.com/fluent/fluentd/releases/tag/v1.17.0)
* SuperDB v0.1
* [Zeek v6.2.1](https://github.com/zeek/zeek/releases/tag/v6.2.1)

### Zeek

The commands below were used to
[install Zeek from a binary package](https://github.com/zeek/zeek/wiki/Binary-Packages).
The [JSON Streaming Logs](https://github.com/corelight/json-streaming-logs)
package was also installed, as this log format is preferred in many production
Zeek environments and it lends itself to use with Fluentd's
[tail input plugin](https://docs.fluentd.org/input/tail).

```
echo 'deb http://download.opensuse.org/repositories/security:/zeek/xUbuntu_24.04/ /' | sudo tee /etc/apt/sources.list.d/security:zeek.list
curl -fsSL https://download.opensuse.org/repositories/security:zeek/xUbuntu_24.04/Release.key | gpg --dearmor | sudo tee /etc/apt/trusted.gpg.d/security_zeek.gpg > /dev/null
sudo apt update
sudo apt install -y zeek
sudo /opt/zeek/bin/zkg install --force json-streaming-logs
```

Two edits were then performed to the configuration:

1. The `#` in the last line in `/opt/zeek/share/zeek/site/local.zeek` was
removed to uncomment `@load packages`, which allows the JSON Streaming Logs
package to be activated when Zeek starts.

2. The file `/opt/zeek/etc/node.cfg` was edited to change the `interface`
setting to reflect the network source from which Zeek should sniff live
traffic, which in our instance was `enX0`.

After making these changes, Zeek was started by running
`sudo /opt/zeek/bin/zeekctl` and executing the `deploy` command.

### SuperDB

The `super`
executable compatible with our instance was downloaded and unpacked to a
directory in our `$PATH`, then the [database service](../../command/db.md#super-db-serve)
was started with a specified storage path.

```
wget https://github.com/superdb/super/releases/download/v1.17.0/zed-v1.17.0.linux-amd64.tar.gz
tar xzvf zed-v1.17.0.linux-amd64.tar.gz
sudo mv zed /usr/local/bin
super db serve -manage 5m $HOME/lake 
```

Once the database service was running, a table was created to hold our Zeek data by
executing the following command in another shell.

```
super db create zeek
```

The default settings when running `super db create` set the
database [sort key](../../command/db.md#sort-key)
to the `ts` field and sort the stored data in descending order by that key.
This configuration is ideal for Zeek log data.

### Fluentd

Multiple approaches are available for
[installing Fluentd](https://docs.fluentd.org/installation).
Here we opted to take the approach of
[installing via Ruby Gem](https://docs.fluentd.org/installation/install-by-gem).

```
sudo apt install -y ruby ruby-dev make gcc
sudo gem install fluentd --no-doc
```

## Simple Example

The following simple `fluentd.conf` was used to watch the streamed Zeek logs
for newly added lines and load each set of them to their table as
a separate [commit](../../database/intro.md#commit-objects).

```
<source>
  @type tail
  path /opt/zeek/logs/current/json_streaming_*
  follow_inodes true
  pos_file /opt/zeek/logs/fluentd.pos
  tag zeek
  <parse>
    @type json
  </parse>
</source>

<match zeek>
  @type http
  endpoint http://127.0.0.1:9867/table/zeek/branch/main
  content_type application/json
  <format>
    @type json
  </format>
</match>
```

When starting Fluentd with this configuration, we followed their
[guidance to increase the maximum number of file descriptors](https://docs.fluentd.org/installation/before-install#increase-the-maximum-number-of-file-descriptors).

```
ulimit -n 65535
sudo fluentd -c fluentd.conf
```

To confirm everything was working, we generated some network traffic that
would be reflected in a Zeek log by performing a DNS lookup from a shell on
our instance.

```
nslookup example.com
```

To see the event was been stored in our table, we executed the following query:

```
super db -S -c 'from zeek | _path=="dns" query=="example.com"'
```

With the Fluentd configuration shown here, it took about a minute for the
most recent log data to be flushed through the ingest flow such that the query
produced the following response:

```
{
    _path: "dns",
    _write_ts: "2024-07-21T00:36:58.826215Z",
    ts: "2024-07-21T00:36:48.826245Z",
    uid: "CVWi4c1GsgQrgUohth",
    "id.orig_h": "172.31.9.104",
    "id.orig_p": 38430,
    "id.resp_h": "172.31.0.2",
    "id.resp_p": 53,
    proto: "udp",
    trans_id: 7250,
    query: "example.com",
    rcode: 0,
    rcode_name: "NOERROR",
    AA: false,
    TC: false,
    RD: false,
    RA: true,
    Z: 0,
    answers: [
        "2606:2800:21f:cb07:6820:80da:af6b:8b2c"
    ],
    TTLs: [
        300.
    ],
    rejected: false
}
```

## Shaping Example

The query result just shown reflects the minimal data typing available in JSON
format. Meanwhile, the
[super data model](../../formats/model.md) provides much
richer data typing options, including some types well-suited to Zeek data such
as `ip`, `time`, and `duration`. In Zed, the task of cleaning up data to
improve its typing is known as [shaping](../../tutorials/shaping.md).

For Zeek data specifically, a
[reference shaper](zeek/shaping.md#reference-shaper-contents)
is available that reflects the field and type information in the logs
generated by a recent Zeek release. To improve the quality of our data, we
next created an expanded configuration that applies the shaper before loading
the data into our table.

First we saved the contents of the shaper from
[here](zeek/shaping.md#reference-shaper-contents) to a file
`shaper.zed`. Then in the same directory we created the following
`fluentd-shaped.conf`:

```
<source>
  @type tail
  path /opt/zeek/logs/current/json_streaming_*
  follow_inodes true
  pos_file /opt/zeek/logs/fluentd.pos
  tag zeek
  <parse>
    @type json
  </parse>
</source>

<match zeek>
  @type exec_filter
  command super -s -I shaper.zed -
  tag shaped
  <format>
    @type json
  </format>
  <parse>
    @type none
  </parse>
  <buffer>
    flush_interval 1s
  </buffer>
</match>

<match shaped>
  @type http
  endpoint http://127.0.0.1:9867/table/zeek-shaped/branch/main
  content_type application/x-sup
  <format>
    @type single_value
  </format>
</match>
```

After stopping the Fluentd process that was previously running, we created a
new table to store these shaped logs and restarted Fluentd with the new
configuration.

```
zed create zeek-shaped
ulimit -n 65535
sudo fluentd -c fluentd-shaped.conf
```

We triggered another Zeek event by performing DNS lookup on another domain.

```
nslookup example.org
```

After a delay, we executed the following query to see the event in its shaped
form:

```
super db -S -c 'from "zeek-shaped" | _path=="dns" query=="example.org"'
```

Example output:

```
{
    _path: "dns",
    ts: 2024-07-21T00:43:38.385932Z,
    uid: "CNcpGS2BFLZaRCyN46",
    id: {
        orig_h: 172.31.9.104,
        orig_p: 42796::(port=uint16),
        resp_h: 172.31.0.2,
        resp_p: 53 (port)
    } (=conn_id),
    proto: "udp" (=zenum),
    trans_id: 19994::uint64,
    rtt: null (duration),
    query: "example.org",
    qclass: null::uint64,
    qclass_name: null::string,
    qtype: null::uint64,
    qtype_name: null::string,
    rcode: 0::uint64,
    rcode_name: "NOERROR",
    AA: false,
    TC: false,
    RD: false,
    RA: true,
    Z: 0::uint64,
    answers: [
        "2606:2800:21f:cb07:6820:80da:af6b:8b2c"
    ],
    TTLs: [
        300ns
    ],
    rejected: false,
    _write_ts: 2024-07-21T00:43:48.396878Z
} (=dns)
```

Notice quotes are no longer present around the values that contain IP addresses
and times, since they are no longer stored as strings. With the data in this
shaped form, we could now invoke [SuperSQL](../../super-sql/intro.md)
functionality that leverages the richer data typing such as filtering `ip`
values by CIDR block, e.g.,

```
super db -c 'from "zeek-shaped" | _path=="conn" | cidr_match(172.31.0.0/16, id.resp_h) | count() by id'
```

which in our test environment produced

```
{id:{orig_h:218.92.0.99,orig_p:9090::(port=uint16),resp_h:172.31.0.253,resp_p:22(port)}(=conn_id),count:4}
{id:{orig_h:172.31.0.253,orig_p:42014::(port=uint16),resp_h:172.31.0.2,resp_p:53(port)}(=conn_id),count:1}
{id:{orig_h:172.31.0.253,orig_p:37490::(port=uint16),resp_h:172.31.0.2,resp_p:53(port)}(=conn_id),count:1}
{id:{orig_h:172.31.0.253,orig_p:33488::(port=uint16),resp_h:172.31.0.2,resp_p:53(port)}(=conn_id),count:1}
{id:{orig_h:172.31.0.253,orig_p:44362::(port=uint16),resp_h:172.31.0.2,resp_p:53(port)}(=conn_id),count:1}
{id:{orig_h:199.83.220.79,orig_p:52876::(port=uint16),resp_h:172.31.0.253,resp_p:22(port)}(=conn_id),count:1}
```

or this query that counts events into buckets by `time` span

```
super db -c 'from "zeek-shaped" | count() by bucket(ts,5m) | sort bucket'
```

which in our test environment produced

```
{bucket:2024-07-19T22:15:00Z,count:1}
{bucket:2024-07-19T22:45:00Z,count:6}
{bucket:2024-07-19T22:50:00Z,count:696}
{bucket:2024-07-19T22:55:00Z,count:683}
{bucket:2024-07-19T23:00:00Z,count:698}
{bucket:2024-07-19T23:05:00Z,count:309}
```

## SuperDB Database Maintenance

The database stores the data for each [`load`](../../command/db.md#super-db-load)
operation in a separate commit. If you observe the output of
`super db log -use zeek-shaped` after several minutes, you will see many
such commits have accumulated, which is a reflection of Fluentd frequently
pushing new sets of lines from each of the many log files generated by Zeek.

The bulky nature of log data combined with the need to often perform "needle
in a haystack" queries over long time spans means that performance could
degrade as many small commits accumulate. However, the `-manage 5m` option
that was included when starting SuperDB service mitigates this effect
by compacting the data in the table every five minutes. This results
in storing the table data across a smaller number of larger
[data objects](../../database/format.md#data-objects), allowing for better query performance
as data volumes increase.

By default, even after compaction is performed, the granular commit history is
still maintained to allow for [time travel](../../database/intro.md#time-travel)
use cases. However, if time travel is not functionality you're likely to
leverage, you can reduce the storage footprint by periodically running
[`super db vacuum`](../../command/db.md#super-db-vacuum). This will delete files from
storage that contain the granular commits that have already been rolled into
larger objects by compaction.

>[!NOTE]
> As described in issue [super/4934](https://github.com/superdb/super/issues/4934),
> even after running `super db vacuum`, some files related to commit history are
> currently still left behind below the database storage path. The issue describes
> manual steps that can be taken to remove these files safely, if desired.
> However, if you find yourself needing to take these steps in your environment,
> please [contact us](#contact-us) as it will allow us to boost the priority
> of addressing the issue.

## Ideas For Enhancement

The examples shown above provide a starting point for creating a production
configuration that suits the needs of your environment. While we cannot
anticipate the many ways users may enhance these configurations, we can cite
some opportunities for possible improvement we spotted during this exercise.
You may wish to experiment with these to see what best suits your needs.

1. **Buffering** - Components of Fluentd used here such as
[`exec_filter`](https://docs.fluentd.org/output/exec_filter) provide many
[buffering](https://docs.fluentd.org/configuration/buffer-section)
options. Varying these may impact how quickly events appear in the table and
the size of the commit objects to which they're initially stored.

2. **BSUP format** - In the [shaping example](#shaping-example) shown above, we
used the [Super (SUP) format](../../formats/sup.md) format for the shaped data output from
[`super`](../../command/super.md). This text format is typically used in contexts
where human readability is required. Due to its compact nature,
[Super Binary (BSUP)](../../formats/bsup.md) format would have been preferred, but in our research
we found Fluentd consistently steered us toward using only text formats.
However, someone more proficient with Fluentd may be able to employ BSUP
instead.

If you have success experimenting with these ideas or have other enhancements
you feel would benefit other users, please [contact us](#contact-us) so this
article can be improved.

## Contact Us

If you're having difficulty, interested in loading or shaping other data
sources, or just have feedback, please join our
[public Slack](https://www.brimdata.io/join-slack/) and speak up or
[open an issue](https://github.com/superdb/super/issues/new/choose). Thanks!
