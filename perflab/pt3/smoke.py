"""单次业务功能验证；不创建 k6 场景或持续业务负载。"""
import http.cookiejar
import json
import socket
import threading
import time
import urllib.request
from environment import STATE, sql, http as es_http

class Client:
    def __init__(self):
        self.cookies=http.cookiejar.CookieJar()
        self.opener=urllib.request.build_opener(urllib.request.ProxyHandler({}),urllib.request.HTTPCookieProcessor(self.cookies))
    def request(self,method,path,data=None):
        payload=None if data is None else json.dumps(data).encode()
        request=urllib.request.Request('http://127.0.0.1:18080'+path,data=payload,method=method)
        if data is not None:request.add_header('Content-Type','application/json')
        with self.opener.open(request,timeout=5) as response:
            body=response.read();return response.status,json.loads(body) if body else {}
    def login(self,index=0):
        credentials=json.loads((STATE/'credentials.json').read_text());user=credentials['users'][index]
        return self.request('POST','/api/v1/auth/login',{'username':user['username'],'password':credentials['password']})[0]

def business_probe(client):
    values={}
    try:
        values['read']=client.request('GET','/api/v1/posts?limit=1')[0]
        status,body=client.request('POST','/api/v1/posts',{'title':'pt3 recovery probe','content':'pt3 recovery probe'})
        values['write']=status
        values['delete']=client.request('DELETE','/api/v1/posts/'+str(body['data']['id']))[0]
    except Exception as error:values['error']=str(error)
    return values

def smoke():
    corpus=json.loads((STATE/'corpus.json').read_text());first=Client();second=Client();results={'started_at':time.time(),'checks':{}}
    results['checks']['login_first']=first.login(0);results['checks']['login_second']=second.login(1)
    for path in ['/api/v1/users/me','/api/v1/posts?limit=1','/api/v1/posts/'+str(corpus['read_post_ids'][0]),'/api/v1/search/posts?q=go&limit=1','/api/v1/notifications?limit=1']:
        results['checks'][path]=first.request('GET',path)[0]
    status,body=first.request('POST','/api/v1/posts',{'title':'pt3 functional smoke','content':'pt3 functional smoke'})
    assert status==201
    post=int(body['data']['id']);path='/api/v1/posts/'+str(post)
    results['checks']['create']=status
    results['checks']['patch']=first.request('PATCH',path,{'title':'pt3 functional edited','content':'pt3 edited body'})[0]
    status,body=first.request('GET',path);assert body['data']['content']=='pt3 edited body'
    results['checks']['read_after_patch']=status
    results['checks']['like']=second.request('PUT',path+'/like')[0]
    results['checks']['bookmark']=second.request('PUT',path+'/bookmark')[0]
    before=int(sql('SELECT COUNT(*) FROM notifications;').strip())
    results['checks']['comment']=second.request('POST',path+'/comments',{'content':'pt3 async notification smoke'})[0]
    for _ in range(100):
        notified=int(sql('SELECT COUNT(*) FROM notifications;').strip())>before
        search=es_http('/gopulse-post-search-v1/_search','POST',{'query':{'term':{'post_id':post}},'size':1})
        hits=search['hits']['hits']
        visible=bool(hits) and hits[0]['_source']['content']=='pt3 edited body'
        backlog=int(sql('SELECT open_requirements FROM business_async_budget;').strip())
        if notified and visible and backlog==0:break
        time.sleep(.2)
    else:raise RuntimeError('异步通知、索引或 requirements 未完成')
    results['notification_applied']=notified;results['search_applied']=visible
    results['checks']['delete']=first.request('DELETE',path)[0]
    for _ in range(100):
        if int(sql('SELECT open_requirements FROM business_async_budget;').strip())==0:break
        time.sleep(.2)
    else:raise RuntimeError('删除后的异步 requirements 未关闭')
    results['finished_at']=time.time()
    (STATE/'smoke.json').write_text(json.dumps(results,indent=2)+'\n')
    print(json.dumps(results,indent=2))

if __name__=='__main__':smoke()
