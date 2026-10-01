import socket
import unittest
from unittest.mock import patch
from phase20_sampler import broker_address, _original_getaddrinfo, kafka_consumer, kafka_offsets, process_stats, container_budget

class SamplerTests(unittest.TestCase):
    def tearDown(self):socket.getaddrinfo=_original_getaddrinfo
    def test_owned_broker_resolution_does_not_change_other_hosts(self):
        with patch('phase20_sampler._original_getaddrinfo') as resolve:
            broker_address('172.30.0.2');socket.getaddrinfo('kafka',19092);socket.getaddrinfo('localhost',80)
            self.assertEqual(resolve.call_args_list[0].args,('172.30.0.2',19092));self.assertEqual(resolve.call_args_list[1].args,('localhost',80))
    def test_observer_never_commits_or_creates_topic(self):
        with patch.dict('sys.modules',{'kafka':__import__('types').SimpleNamespace(__version__='2.2.15',KafkaConsumer=__import__('unittest.mock').mock.Mock())}):
            import kafka
            kafka_consumer('172.30.0.2','product-group')
            kwargs=kafka.KafkaConsumer.call_args.kwargs
            self.assertFalse(kwargs['enable_auto_commit']);self.assertFalse(kwargs['allow_auto_create_topics']);self.assertLessEqual(kwargs['request_timeout_ms'],3000)
    def test_process_sampling_missing_is_not_zero_success(self):
        self.assertIsNone(process_stats(99999999));self.assertIsNone(process_stats(None))
    def test_container_budget_keeps_rss_and_memory_limit_separate(self):
        value=container_budget({'HostConfig':{'Memory':134217728,'NanoCpus':250000000},'State':{'Pid':0}},65536)
        self.assertEqual(value['memory_current_bytes'],65536)
        self.assertEqual(value['memory_limit_bytes'],134217728)
        self.assertEqual(value['cpu_quota_cores'],0.25)
        self.assertIsNone(value['container_pid'])
    def test_kafka_offsets_retries_one_transient_timeout(self):
        class Client:
            def __init__(self):self.calls=0
            def end_offsets(self,partitions):
                self.calls+=1
                if self.calls==1:raise RuntimeError('temporary timeout')
                return {partitions[0]:7}
            def committed(self,partition):return 6
        partition=object();client=Client()
        end,committed=kafka_offsets(client,[partition])
        self.assertEqual(client.calls,2);self.assertEqual(end[partition],7);self.assertEqual(committed[partition],6)
