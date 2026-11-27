# JSON Logs

Zeek events may be logged to JSON as an alternative to its TSV format
using two common configurations:

* the [JSON Streaming Logs](https://github.com/corelight/json-streaming-logs)
   package (recommended for use with `super`), or
* the built-in [ASCII logger](https://docs.zeek.org/en/current/scripts/base/frameworks/logging/writers/ascii.zeek.html)
   configured with `redef LogAscii::use_json = T;`

When reading these JSON logs,
much of the rich data typing that was originally present inside Zeek is lost.
This detail can be restored using SuperSQL scripts
to restore the type-richness of Zeek data.

In both cases, `super` can read these logs automatically
as is, but with caveats.

Let's have a look at the [conn record](logs.md#connlog) from TSV example,
but now as generated using the JSON Streaming Logs package.

### conn.json

```mdtest-input conn.json
{"_path":"conn","_write_ts":"2018-03-24T17:15:21.400275Z","ts":"2018-03-24T17:15:21.255387Z","uid":"C8Tful1TvM3Zf5x8fl","id.orig_h":"10.164.94.120","id.orig_p":39681,"id.resp_h":"10.47.3.155","id.resp_p":3389,"proto":"tcp","duration":0.004266023635864258,"orig_bytes":97,"resp_bytes":19,"conn_state":"RSTR","missed_bytes":0,"history":"ShADTdtr","orig_pkts":10,"orig_ip_bytes":730,"resp_pkts":6,"resp_ip_bytes":342}
```

### Example

```mdtest-command
super -S -c 'head 1' conn.json
```

### Output
```mdtest-output
{
  _path: "conn",
  _write_ts: "2018-03-24T17:15:21.400275Z",
  ts: "2018-03-24T17:15:21.255387Z",
  uid: "C8Tful1TvM3Zf5x8fl",
  "id.orig_h": "10.164.94.120",
  "id.orig_p": 39681,
  "id.resp_h": "10.47.3.155",
  "id.resp_p": 3389,
  proto: "tcp",
  duration: 0.004266023635864258,
  orig_bytes: 97,
  resp_bytes: 19,
  conn_state: "RSTR",
  missed_bytes: 0,
  history: "ShADTdtr",
  orig_pkts: 10,
  orig_ip_bytes: 730,
  resp_pkts: 6,
  resp_ip_bytes: 342
}
```

When we compare this to the TSV example, we notice a few things right away that
all follow from the records having been previously output as JSON.

1. The timestamps like `_write_ts` and `ts` are printed as strings rather than
   the  Super `time` type.
2. The IP addresses such as `id.orig_h` and `id.resp_h` are printed as strings
   rather than the Super `ip` type.
3. The connection `duration` is printed as a floating point number rather than
   the Super `duration` type.
4. The keys for the null-valued fields in the record read from
   TSV are not present in the record read from JSON.

If you're familiar with the limitations of the JSON data types, it makes sense
that Zeek chose to output these values as it did. Furthermore, if
you were just seeking to do quick searches on the string values or simple math
on the numbers, these limitations may be acceptable. However, if you intended
to perform operations like
[aggregations with time-based grouping](../../../super-sql/functions/time/bucket.md)
or [CIDR matches](../../../super-sql/functions/network/network_of.md)
on IP addresses, you would likely want to restore the rich types as
the records are being read. The document on [shaping Zeek JSON](shaping.md)
provides details on how this can be done.

## Version Compatibility

The fields and data types in the reference shaper reflect the default
JSON-format logs output by Zeek releases up to the version number referenced
in the comments at the top. They have been revisited periodically
as new Zeek versions have been released.

Most changes we've observed in Zeek logs between versions have involved only the
addition of new fields. Because of this, we expect the shaper should be usable
as is for Zeek releases older than the one most recently tested, since fields
in the shaper not present in your environment would just be filled in with
`null` values.

All attempts will be made to update this reference shaper in a timely manner
as new Zeek versions are released. However, if you have modified your Zeek
installation with [packages](https://packages.zeek.org/)
or other customizations, or if you are using a [Corelight Sensor](https://corelight.com/products/appliance-sensors/)
that produces Zeek logs with many fields and logs beyond those found in open
source Zeek, the reference shaper will not cover all the fields in your logs.
[As described below](#supersql-pipeline), by default the shaper will produce errors
when this happens, though it can also be configured to silently crop such
fields or keep them while assigning inferred types.

## Reference Shaper Contents

The following reference `shaper.spq` may seem large, but ultimately it follows a
fairly simple pattern that repeats across the many [Zeek log types](https://docs.zeek.org/en/current/reference/zeekscript/log-files.html).

```mdtest-input shaper.spq
-- This reference shaper for Zeek JSON logs was most recently tested with
-- Zeek v7.0.0. The fields and data types reflect the default JSON
-- logs output by that Zeek version when using the JSON Streaming Logs package.
-- (https://github.com/corelight/json-streaming-logs).

const _crop_records = true
const _error_if_cropped = true

type port=uint16
type zenum=string
type conn_id={orig_h:ip,orig_p:port,resp_h:ip,resp_p:port}

const zeek_log_types = |{
  "analyzer": <analyzer={_path:string,ts:time,cause:string,analyzer_kind:string,analyzer_name:string,uid:string,fuid:string,id:conn_id,failure_reason:string,failure_data:string,_write_ts:time}>,
  "broker": <broker={_path:string,ts:time,ty:zenum,ev:string,peer:{address:string,bound_port:port},message:string,_write_ts:time}>,
  "capture_loss": <capture_loss={_path:string,ts:time,ts_delta:duration,peer:string,gaps:uint64,acks:uint64,percent_lost:float64,_write_ts:time}>,
  "cluster": <cluster={_path:string,ts:time,node:string,message:string,_write_ts:time}>,
  "config": <config={_path:string,ts:time,id:string,old_value:string,new_value:string,location:string,_write_ts:time}>,
  "conn": <conn={_path:string,ts:time,uid:string,id:conn_id,proto:zenum,service:string,duration:duration,orig_bytes:uint64,resp_bytes:uint64,conn_state:string,local_orig:bool,local_resp:bool,missed_bytes:uint64,history:string,orig_pkts:uint64,orig_ip_bytes:uint64,resp_pkts:uint64,resp_ip_bytes:uint64,tunnel_parents:|[string]|,_write_ts:time}>,
  "dce_rpc": <dce_rpc={_path:string,ts:time,uid:string,id:conn_id,rtt:duration,named_pipe:string,endpoint:string,operation:string,_write_ts:time}>,
  "dhcp": <dhcp={_path:string,ts:time,uids:|[string]|,client_addr:ip,server_addr:ip,mac:string,host_name:string,client_fqdn:string,domain:string,requested_addr:ip,assigned_addr:ip,lease_time:duration,client_message:string,server_message:string,msg_types:[string],duration:duration,_write_ts:time}>,
  "dnp3": <dnp3={_path:string,ts:time,uid:string,id:conn_id,fc_request:string,fc_reply:string,iin:uint64,_write_ts:time}>,
  "dns": <dns={_path:string,ts:time,uid:string,id:conn_id,proto:zenum,trans_id:uint64,rtt:duration,query:string,qclass:uint64,qclass_name:string,qtype:uint64,qtype_name:string,rcode:uint64,rcode_name:string,AA:bool,TC:bool,RD:bool,RA:bool,Z:uint64,answers:[string],TTLs:[duration],rejected:bool,_write_ts:time}>,
  "dpd": <dpd={_path:string,ts:time,uid:string,id:conn_id,proto:zenum,analyzer:string,failure_reason:string,_write_ts:time}>,
  "files": <files={_path:string,ts:time,fuid:string,uid:string,id:conn_id,source:string,depth:uint64,analyzers:|[string]|,mime_type:string,filename:string,duration:duration,local_orig:bool,is_orig:bool,seen_bytes:uint64,total_bytes:uint64,missing_bytes:uint64,overflow_bytes:uint64,timedout:bool,parent_fuid:string,md5:string,sha1:string,sha256:string,extracted:string,extracted_cutoff:bool,extracted_size:uint64,_write_ts:time}>,
  "ftp": <ftp={_path:string,ts:time,uid:string,id:conn_id,user:string,password:string,command:string,arg:string,mime_type:string,file_size:uint64,reply_code:uint64,reply_msg:string,data_channel:{passive:bool,orig_h:ip,resp_h:ip,resp_p:port},fuid:string,_write_ts:time}>,
  "http": <http={_path:string,ts:time,uid:string,id:conn_id,trans_depth:uint64,method:string,host:string,uri:string,referrer:string,version:string,user_agent:string,origin:string,request_body_len:uint64,response_body_len:uint64,status_code:uint64,status_msg:string,info_code:uint64,info_msg:string,tags:|[zenum]|,username:string,password:string,proxied:|[string]|,orig_fuids:[string],orig_filenames:[string],orig_mime_types:[string],resp_fuids:[string],resp_filenames:[string],resp_mime_types:[string],_write_ts:time}>,
  "intel": <intel={_path:string,ts:time,uid:string,id:conn_id,seen:{indicator:string,indicator_type:zenum,where:zenum,node:string},matched:|[zenum]|,sources:|[string]|,fuid:string,file_mime_type:string,file_desc:string,_write_ts:time}>,
  "irc": <irc={_path:string,ts:time,uid:string,id:conn_id,nick:string,user:string,command:string,value:string,addl:string,dcc_file_name:string,dcc_file_size:uint64,dcc_mime_type:string,fuid:string,_write_ts:time}>,
  "kerberos": <kerberos={_path:string,ts:time,uid:string,id:conn_id,request_type:string,client:string,service:string,success:bool,error_msg:string,from:time,till:time,cipher:string,forwardable:bool,renewable:bool,client_cert_subject:string,client_cert_fuid:string,server_cert_subject:string,server_cert_fuid:string,_write_ts:time}>,
  "known_certs": <known_certs={_path:string,ts:time,host:ip,port_num:port,subject:string,issuer_subject:string,serial:string,_write_ts:time}>,
  "known_hosts": <known_hosts={_path:string,ts:time,host:ip,_write_ts:time}>,
  "known_services": <known_services={_path:string,ts:time,host:ip,port_num:port,port_proto:zenum,service:|[string]|,_write_ts:time}>,
  "ldap": <ldap={_path:string,ts:time,uid:string,id:conn_id,message_id:int64,version:int64,opcode:string,result:string,diagnostic_message:string,object:string,argument:string,_write_ts:time}>,
  "ldap_search": <ldap_search={_path:string,ts:time,uid:string,id:conn_id,message_id:int64,scope:string,deref_aliases:string,base_object:string,result_count:uint64,result:string,diagnostic_message:string,filter:string,attributes:[string],_write_ts:time}>,
  "loaded_scripts": <loaded_scripts={_path:string,name:string,_write_ts:time}>,
  "modbus": <modbus={_path:string,ts:time,uid:string,id:conn_id,tid:uint64,unit:uint64,func:string,pdu_type:string,exception:string,_write_ts:time}>,
  "mqtt_connect": <mqtt_connect={_path:string,ts:time,uid:string,id:conn_id,proto_name:string,proto_version:string,client_id:string,connect_status:string,will_topic:string,will_payload:string,_write_ts:time}>,
  "mqtt_publish": <mqtt_publish={_path:string,ts:time,uid:string,id:conn_id,from_client:bool,retain:bool,qos:string,status:string,topic:string,payload:string,payload_len:uint64,_write_ts:time}>,
  "mqtt_subscribe": <mqtt_subscribe={_path:string,ts:time,uid:string,id:conn_id,action:zenum,topics:[string],qos_levels:[uint64],granted_qos_level:uint64,ack:bool,_write_ts:time}>,
  "mysql": <mysql={_path:string,ts:time,uid:string,id:conn_id,cmd:string,arg:string,success:bool,rows:uint64,response:string,_write_ts:time}>,
  "netcontrol": <netcontrol={_path:string,ts:time,rule_id:string,category:zenum,cmd:string,state:zenum,action:string,target:zenum,entity_type:string,entity:string,mod:string,msg:string,priority:int64,expire:duration,location:string,plugin:string,_write_ts:time}>,
  "netcontrol_drop": <netcontrol_drop={_path:string,ts:time,rule_id:string,orig_h:ip,orig_p:port,resp_h:ip,resp_p:port,expire:duration,location:string,_write_ts:time}>,
  "netcontrol_shunt": <netcontrol_shunt={_path:string,ts:time,rule_id:string,f:{src_h:ip,src_p:port,dst_h:ip,dst_p:port},expire:duration,location:string,_write_ts:time}>,
  "notice": <notice={_path:string,ts:time,uid:string,id:conn_id,fuid:string,file_mime_type:string,file_desc:string,proto:zenum,note:zenum,msg:string,sub:string,src:ip,dst:ip,p:port,n:uint64,peer_descr:string,actions:|[zenum]|,email_dest:|[string]|,suppress_for:duration,remote_location:{country_code:string,region:string,city:string,latitude:float64,longitude:float64},_write_ts:time}>,
  "notice_alarm": <notice_alarm={_path:string,ts:time,uid:string,id:conn_id,fuid:string,file_mime_type:string,file_desc:string,proto:zenum,note:zenum,msg:string,sub:string,src:ip,dst:ip,p:port,n:uint64,peer_descr:string,actions:|[zenum]|,email_dest:|[string]|,suppress_for:duration,remote_location:{country_code:string,region:string,city:string,latitude:float64,longitude:float64},_write_ts:time}>,
  "ntlm": <ntlm={_path:string,ts:time,uid:string,id:conn_id,username:string,hostname:string,domainname:string,server_nb_computer_name:string,server_dns_computer_name:string,server_tree_name:string,success:bool,_write_ts:time}>,
  "ntp": <ntp={_path:string,ts:time,uid:string,id:conn_id,version:uint64,mode:uint64,stratum:uint64,poll:duration,precision:duration,root_delay:duration,root_disp:duration,ref_id:string,ref_time:time,org_time:time,rec_time:time,xmt_time:time,num_exts:uint64,_write_ts:time}>,
  "ocsp": <ocsp={_path:string,ts:time,id:string,hashAlgorithm:string,issuerNameHash:string,issuerKeyHash:string,serialNumber:string,certStatus:string,revoketime:time,revokereason:string,thisUpdate:time,nextUpdate:time,_write_ts:time}>,
  "openflow": <openflow={_path:string,ts:time,dpid:uint64,match:{in_port:uint64,dl_src:string,dl_dst:string,dl_vlan:uint64,dl_vlan_pcp:uint64,dl_type:uint64,nw_tos:uint64,nw_proto:uint64,nw_src:net,nw_dst:net,tp_src:uint64,tp_dst:uint64},flow_mod:{cookie:uint64,table_id:uint64,command:zenum,idle_timeout:uint64,hard_timeout:uint64,priority:uint64,out_port:uint64,out_group:uint64,flags:uint64,actions:{out_ports:[uint64],vlan_vid:uint64,vlan_pcp:uint64,vlan_strip:bool,dl_src:string,dl_dst:string,nw_tos:uint64,nw_src:ip,nw_dst:ip,tp_src:uint64,tp_dst:uint64}},_write_ts:time}>,
  "packet_filter": <packet_filter={_path:string,ts:time,node:string,filter:string,init:bool,success:bool,failure_reason:string,_write_ts:time}>,
  "pe": <pe={_path:string,ts:time,id:string,machine:string,compile_ts:time,os:string,subsystem:string,is_exe:bool,is_64bit:bool,uses_aslr:bool,uses_dep:bool,uses_code_integrity:bool,uses_seh:bool,has_import_table:bool,has_export_table:bool,has_cert_table:bool,has_debug_data:bool,section_names:[string],_write_ts:time}>,
  "quic": <quic={_path:string,ts:time,uid:string,id:conn_id,version:string,client_initial_dcid:string,client_scid:string,server_scid:string,server_name:string,client_protocol:string,history:string,_write_ts:time}>,
  "radius": <radius={_path:string,ts:time,uid:string,id:conn_id,username:string,mac:string,framed_addr:ip,tunnel_client:string,connect_info:string,reply_msg:string,result:string,ttl:duration,_write_ts:time}>,
  "rdp": <rdp={_path:string,ts:time,uid:string,id:conn_id,cookie:string,result:string,security_protocol:string,client_channels:[string],keyboard_layout:string,client_build:string,client_name:string,client_dig_product_id:string,desktop_width:uint64,desktop_height:uint64,requested_color_depth:string,cert_type:string,cert_count:uint64,cert_permanent:bool,encryption_level:string,encryption_method:string,_write_ts:time}>,
  "reporter": <reporter={_path:string,ts:time,level:zenum,message:string,location:string,_write_ts:time}>,
  "rfb": <rfb={_path:string,ts:time,uid:string,id:conn_id,client_major_version:string,client_minor_version:string,server_major_version:string,server_minor_version:string,authentication_method:string,auth:bool,share_flag:bool,desktop_name:string,width:uint64,height:uint64,_write_ts:time}>,
  "signatures": <signatures={_path:string,ts:time,uid:string,src_addr:ip,src_port:port,dst_addr:ip,dst_port:port,note:zenum,sig_id:string,event_msg:string,sub_msg:string,sig_count:uint64,host_count:uint64,_write_ts:time}>,
  "sip": <sip={_path:string,ts:time,uid:string,id:conn_id,trans_depth:uint64,method:string,uri:string,date:string,request_from:string,request_to:string,response_from:string,response_to:string,reply_to:string,call_id:string,seq:string,subject:string,request_path:[string],response_path:[string],user_agent:string,status_code:uint64,status_msg:string,warning:string,request_body_len:uint64,response_body_len:uint64,content_type:string,_write_ts:time}>,
  "smb_files": <smb_files={_path:string,ts:time,uid:string,id:conn_id,fuid:string,action:zenum,path:string,name:string,size:uint64,prev_name:string,times:{modified:time,accessed:time,created:time,changed:time},_write_ts:time}>,
  "smb_mapping": <smb_mapping={_path:string,ts:time,uid:string,id:conn_id,path:string,service:string,native_file_system:string,share_type:string,_write_ts:time}>,
  "smtp": <smtp={_path:string,ts:time,uid:string,id:conn_id,trans_depth:uint64,helo:string,mailfrom:string,rcptto:|[string]|,date:string,from:string,to:|[string]|,cc:|[string]|,reply_to:string,msg_id:string,in_reply_to:string,subject:string,x_originating_ip:ip,first_received:string,second_received:string,last_reply:string,path:[ip],user_agent:string,tls:bool,fuids:[string],is_webmail:bool,_write_ts:time}>,
  "snmp": <snmp={_path:string,ts:time,uid:string,id:conn_id,duration:duration,version:string,community:string,get_requests:uint64,get_bulk_requests:uint64,get_responses:uint64,set_requests:uint64,display_string:string,up_since:time,_write_ts:time}>,
  "socks": <socks={_path:string,ts:time,uid:string,id:conn_id,version:uint64,user:string,password:string,status:string,request:{host:ip,name:string},request_p:port,bound:{host:ip,name:string},bound_p:port,_write_ts:time}>,
  "software": <software={_path:string,ts:time,host:ip,host_p:port,software_type:zenum,name:string,version:{major:uint64,minor:uint64,minor2:uint64,minor3:uint64,addl:string},unparsed_version:string,_write_ts:time}>,
  "ssh": <ssh={_path:string,ts:time,uid:string,id:conn_id,version:uint64,auth_success:bool,auth_attempts:uint64,direction:zenum,client:string,server:string,cipher_alg:string,mac_alg:string,compression_alg:string,kex_alg:string,host_key_alg:string,host_key:string,remote_location:{country_code:string,region:string,city:string,latitude:float64,longitude:float64},_write_ts:time}>,
  "ssl": <ssl={_path:string,ts:time,uid:string,id:conn_id,version:string,cipher:string,curve:string,server_name:string,resumed:bool,last_alert:string,next_protocol:string,established:bool,ssl_history:string,cert_chain_fps:[string],client_cert_chain_fps:[string],subject:string,issuer:string,client_subject:string,client_issuer:string,sni_matches_cert:bool,validation_status:string,_write_ts:time}>,
  "stats": <stats={_path:string,ts:time,peer:string,mem:uint64,pkts_proc:uint64,bytes_recv:uint64,pkts_dropped:uint64,pkts_link:uint64,pkt_lag:duration,pkts_filtered:uint64,events_proc:uint64,events_queued:uint64,active_tcp_conns:uint64,active_udp_conns:uint64,active_icmp_conns:uint64,tcp_conns:uint64,udp_conns:uint64,icmp_conns:uint64,timers:uint64,active_timers:uint64,files:uint64,active_files:uint64,dns_requests:uint64,active_dns_requests:uint64,reassem_tcp_size:uint64,reassem_file_size:uint64,reassem_frag_size:uint64,reassem_unknown_size:uint64,_write_ts:time}>,
  "syslog": <syslog={_path:string,ts:time,uid:string,id:conn_id,proto:zenum,facility:string,severity:string,message:string,_write_ts:time}>,
  "telemetry_histogram": <telemetry_histogram={_path:string,ts:time,peer:string,name:string,labels:[string],label_values:[string],bounds:[float64],values:[float64],sum:float64,observations:float64,_write_ts:time}>,
  "telemetry": <telemetry={_path:string,ts:time,peer:string,metric_type:string,name:string,labels:[string],label_values:[string],value:float64,_write_ts:time}>,
  "tunnel": <tunnel={_path:string,ts:time,uid:string,id:conn_id,tunnel_type:zenum,action:zenum,_write_ts:time}>,
  "websocket": <websocket={_path:string,ts:time,uid:string,id:conn_id,host:string,uri:string,user_agent:string,subprotocol:string,client_protocols:[string],server_extensions:[string],client_extensions:[string],_write_ts:time}>,
  "weird": <weird={_path:string,ts:time,uid:string,id:conn_id,name:string,addl:string,notice:bool,peer:string,source:string,_write_ts:time}>,
  "x509": <x509={_path:string,ts:time,fingerprint:string,certificate:{version:uint64,serial:string,subject:string,issuer:string,not_valid_before:time,not_valid_after:time,key_alg:string,sig_alg:string,key_type:string,key_length:uint64,exponent:string,curve:string},san:{dns:[string],uri:[string],email:[string],ip:[ip]},basic_constraints:{ca:bool,path_len:uint64},host_cert:bool,client_cert:bool,_write_ts:time}>
}|

values nest_dotted(this)
| switch has(_path)
  case true (
    switch (_path in zeek_log_types)
    case true (
      values {_original: this, _shaped: shape(this, zeek_log_types[_path])}
      | switch has_error(_shaped)
        case true (
          values error({msg: "shaper error(s): see inner error value(s) for details", _original, _shaped})
        )
        case false (
          values {_original, _shaped}
          | switch _crop_records
            case true (
              put _cropped := crop(_shaped, zeek_log_types[_shaped._path])
              | switch (_cropped == _shaped)
                case true ( values _shaped )
                case false (
                  values {_original, _shaped, _cropped}
                  | switch _error_if_cropped
                    case true (
                      values error({msg: "shaper error: one or more fields were cropped", _original, _shaped, _cropped})
                    )
                    case false ( values _cropped )
                )
            )
            case false ( values _shaped )
        )
    )
    case false (
        values error({msg: "shaper error: _path '" + _path + "' is not a known zeek log type in shaper config", _original: this})
    )
  )
  case false (
    values error({msg: "shaper error: input record lacks _path field", _original: this})
  )
```

### Configurable Options

The shaper begins with some configurable boolean constants that control how
the shaper will behave when the JSON data does not precisely match the Zeek
type definitions.

* `_crop_records` (default: `true`) - Fields in the JSON records whose names
are not referenced in the type definitions will be removed. If set to `false`,
such a field would be maintained and assigned an inferred type.

* `_error_if_cropped` (default: `true`) - If such a field is cropped, the
original input record will be
wrapped inside an error value
along with the shaped and cropped variations.

At these default settings, the shaper is well-suited for an iterative workflow
with a goal of establishing full coverage of the JSON data with rich types.
For instance, the [`has_error` function](../../../super-sql/functions/errors/has_error.md)
can be applied on the shaped output and any error values surfaced will point
to fields that can be added to the type definitions in the shaper.

### Leading Type Definitions

The next three lines define types that are referenced further below in the
type definitions for the different Zeek events.

```
type port=uint16;
type zenum=string;
type conn_id={orig_h:ip,orig_p:port,resp_h:ip,resp_p:port};
```
The `port` and `zenum` types are described further in the
[Zeek Type Compatibility](types.md) doc.
The `conn_id` type will just save us from having to repeat these fields
individually in the many Zeek record types that contain an embedded `id`
record.

### Type Definitions Per Zeek Log `_path`

The bulk of this shaper consists of detailed per-field data type
definitions for each record in the default set of JSON logs output by Zeek.
These type definitions reference the types we defined above, such as `port`
and `conn_id`. The syntax for defining primitive and complex types follows the
relevant sections of the [Super (SUP) format](../../../formats/sup.md#2-the-sup-format)
specification.

```
...
  "conn": <conn={_path:string,ts:time,uid:string,id:conn_id,proto:zenum,service:string,duration:duration,orig_bytes:uint64,resp_bytes:uint64,conn_state:string,local_orig:bool,local_resp:bool,missed_bytes:uint64,history:string,orig_pkts:uint64,orig_ip_bytes:uint64,resp_pkts:uint64,resp_ip_bytes:uint64,tunnel_parents:|[string]|,_write_ts:time}>,
  "dce_rpc": <dce_rpc={_path:string,ts:time,uid:string,id:conn_id,rtt:duration,named_pipe:string,endpoint:string,operation:string,_write_ts:time}>,
...
```

>[!NOTE]
> See [the role of `_path`](logs.md#the-role-of-_path)
> for important details if you're using Zeek's built-in [ASCII logger](https://docs.zeek.org/en/current/scripts/base/frameworks/logging/writers/ascii.zeek.html)
> rather than the
> [JSON Streaming Logs](https://github.com/corelight/json-streaming-logs) package.

### SuperSQL Pipeline

The shaper ends with a pipeline that stitches together everything we've defined
so far.

```
values nest_dotted(this)
| switch has(_path) (
  case true => switch (_path in zeek_log_types) (
    case true => values {_original: this, _shaped: shape(zeek_log_types[_path])}
      | switch has_error(_shaped) (
          case true => values error({msg: "shaper error(s): see inner error value(s) for details", _original, _shaped})
          case false => values {_original, _shaped}
            | switch _crop_records (
                case true => put _cropped := crop(_shaped, zeek_log_types[_shaped._path])
                  | switch (_cropped == _shaped) (
                      case true => values _shaped
                      case false => values {_original, _shaped, _cropped}
                      | switch _error_if_cropped (
                          case true => values error({msg: "shaper error: one or more fields were cropped", _original, _shaped, _cropped})
                          case false => values _cropped
                        )
                  )
                case false => values _shaped
            )
      )
    case false => values error({msg: "shaper error: _path '" + _path + "' is not a known zeek log type in shaper config", _original: this})
  )
  case false => values error({msg: "shaper error: input record lacks _path field", _original: this})
)
```

Picking this apart, it transforms each record as it's being read in several
steps.

1. The [nest_dotted](../../../super-sql/functions/records/nest_dotted.md) function
   reverses the Zeek JSON logger's "flattening" of nested records, e.g., how
   it populates a field named `id.orig_h` rather than creating a field `id` with
   sub-field `orig_h` inside it. Restoring the original nesting now gives us
   the option to reference the embedded record named `id` in SuperSQL
   and access the entire 4-tuple of values, but still access the individual
   values using the same dotted syntax like `id.orig_h` when needed.

2. The [switch](../../../super-sql/operators/switch.md) operator is used to flag
   any problems encountered when applying the shaper logic, e.g.,

   * An incoming Zeek JSON record has a `_path` value for which the shaper
    lacks a type definition.
   * A field in an incoming Zeek JSON record is located in our type
     definitions but cannot be successfully
     [cast](../../../super-sql/functions/types/cast.md)
     to the target type defined in the shaper.
   * An incoming Zeek JSON record has additional field(s) beyond those in
     the target type definition and the [configurable options](#configurable-options)
     are set such that this should be treated as an error.

3. Each `shape` function **TODO: delete** call applies an
   appropriate type definition based on the nature of the incoming Zeek JSON
   record. The logic of `shape` includes:

   * For any fields referenced in the type definition that aren't present in
     the input record, the field is added with a `null` value.
   * Each field in the input record is cast to the corresponding type of the
     field of the same name in the type definition.
   * The fields in the input record are ordered to match the order in which
     they appear in the type definition.

## Invoking the Shaper From `super`

A shaper is typically invoked via the `-I` option of `super`.

For example, if we assume this input file `weird.json`

```mdtest-input weird.json
{
  "_path": "weird",
  "_write_ts": "2018-03-24T17:15:20.600843Z",
  "ts": "2018-03-24T17:15:20.600843Z",
  "uid": "C1zOivgBT6dBmknqk",
  "id.orig_h": "10.47.1.152",
  "id.orig_p": 49562,
  "id.resp_h": "23.217.103.245",
  "id.resp_p": 80,
  "name": "TCP_ack_underflow_or_misorder",
  "notice": false,
  "peer": "zeek"
}
```

applying the reference shaper via

```mdtest-command-skip
super -S -I shaper.spq weird.json
```

produces

```mdtest-output-skip
{
    _path: "weird",
    ts: 2018-03-24T17:15:20.600843Z,
    uid: "C1zOivgBT6dBmknqk",
    id: {
        orig_h: 10.47.1.152,
        orig_p: 49562::(port=uint16),
        resp_h: 23.217.103.245,
        resp_p: 80::port
    }::=conn_id,
    name: "TCP_ack_underflow_or_misorder",
    addl: null::string,
    notice: false,
    peer: "zeek",
    source: null::string,
    _write_ts: 2018-03-24T17:15:20.600843Z
}::=weird
```

If working in a directory containing many JSON logs, the
reference shaper can be applied to all the records they contain and
output them all in a single [Super Binary (BSUP)](../../../formats/bsup.md) file as
follows:

```
super -I shaper.spq *.log > /tmp/all.bsup
```

If you wish to apply the shaper and then perform additional
operations on the richly-typed records, the query on the command line
should begin with a `|`, as this appends it to the pipeline at the bottom of
the shaper from the included file.

For example, to see a SUP representation of just the errors that may have
come from attempting to shape all the logs in the current directory:

```
super -S -I shaper.spq -c '| has_error(this)' *.log
```

## Importing Shaped Data Into Zui

If you wish to shape your Zeek JSON data in [Zui](https://zui.brimdata.io/),
drag the files into the app and then paste the contents of the
[`shaper.spq` shown above](#reference-shaper-contents) into the
**Shaper Editor** of the [**Preview & Load**](https://zui.brimdata.io/docs/features/Preview-Load)
screen.

## Contact us!

If you're having difficulty, interested in shaping other data sources, or
just have feedback, please join our [public Slack](https://www.brimdata.io/join-slack/)
and speak up or [open an issue](https://github.com/superdb/super/issues/new/choose).
Thanks!
