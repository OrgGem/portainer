"""Opt-in regressions for shared-tag races, managed builder protection and unsafe cgroups.
Requires the delay proxy binary at .tmp/review-delay-proxy (see BUILD-POLICY.md)."""
import hashlib, json, os, pathlib, secrets, subprocess, time, urllib.request, urllib.error, uuid

def docker(*args):
    return subprocess.check_output(['docker',*args], text=True, timeout=240).strip()

def main():
    prefix='build-review-'+uuid.uuid4().hex[:8]
    net,server,proxy,dind,data=[prefix+s for s in ('-net','-server','-proxy','-dind','-data')]
    info=json.loads(docker('info','--format','{{json .}}'))
    builder='buildx_buildkit_portainer-compose-'+hashlib.sha256(info['ID'].encode()).hexdigest()[:20]
    if subprocess.run(['docker','inspect',builder],capture_output=True).returncode==0:
        raise RuntimeError('existing managed builder: use isolated daemon')
    base, token='',None
    setup=secrets.token_hex(32)
    stacks,images=[],[]
    def api(method,path,body=None,expected=200):
        headers={'Content-Type':'application/json'}
        if token: headers['Authorization']='Bearer '+token
        if path=='/users/admin/init':headers['X-Setup-Token']=setup
        req=urllib.request.Request(base+'/api'+path,headers=headers,method=method,data=None if body is None else json.dumps(body).encode())
        try:
            with urllib.request.urlopen(req,timeout=30) as r: code,text=r.status,r.read()
        except urllib.error.HTTPError as e: code,text=e.code,e.read()
        assert code==expected,(path,code,text[:1000])
        return json.loads(text) if text else None
    def endpoint(name,url):
        boundary=uuid.uuid4().hex
        fields={'Name':name,'URL':url,'EndpointCreationType':'1'}
        body=''.join(f'--{boundary}\r\nContent-Disposition: form-data; name="{k}"\r\n\r\n{v}\r\n' for k,v in fields.items())+f'--{boundary}--\r\n'
        req=urllib.request.Request(base+'/api/endpoints',data=body.encode(),headers={'Authorization':'Bearer '+token,'Content-Type':'multipart/form-data; boundary='+boundary},method='POST')
        with urllib.request.urlopen(req,timeout=60) as r:return json.load(r)['Id']
    def build(suffix,env,image,instruction,baseimage='alpine:3.20'):
        name=prefix+'-'+suffix
        text=f'services:\n  app:\n    image: {image}\n    build:\n      context: .\n      no_cache: true\n      dockerfile_inline: |\n        FROM {baseimage}\n        RUN {instruction}\n    command: ["sleep", "600"]\n'
        result=api('POST',f'/stacks/create/standalone/string?endpointId={env}',{'Name':name,'StackFileContent':text,'Env':[]})
        stacks.append(name);return result['Id']
    def wait(sid):
        for _ in range(180):
            s=api('GET',f'/stacks/{sid}')
            if s['Status']!=3:
                assert s['Status']==1,s.get('DeploymentStatus');return s
            time.sleep(1)
        raise RuntimeError('stack build timed out')
    try:
        docker('network','create',net)
        docker('volume','create',data)
        docker('run','-d','--name',proxy,'--network',net,'-p','127.0.0.1::8080','-e','BLOCK_NAME='+prefix+'-a-app','-v','/var/run/docker.sock:/var/run/docker.sock','-v',str(pathlib.Path('.tmp/review-delay-proxy').resolve())+':/review-proxy:ro','--entrypoint','/review-proxy',os.environ.get('PORTAINER_TEST_IMAGE', 'portainer-custom:2.45.3-build-policy-fixes'))
        proxybase='http://'+docker('port',proxy,'8080/tcp')
        docker('run','-d','--name',server,'--network',net,'-p','127.0.0.1::9000','-v','/var/run/docker.sock:/var/run/docker.sock','-v',data+':/data',os.environ.get('PORTAINER_TEST_IMAGE', 'portainer-custom:2.45.3-build-policy-fixes'),'--http-enabled','--setup-token',setup)
        base='http://'+docker('port',server,'9000/tcp')
        for _ in range(60):
            try: api('GET','/system/status');break
            except OSError:time.sleep(1)
        password=secrets.token_urlsafe(24)
        api('POST','/users/admin/init',{'Username':'admin','Password':password})
        token=api('POST','/auth',{'Username':'admin','Password':password})['jwt']
        local=endpoint('local','unix:///var/run/docker.sock')
        delayed=endpoint('delayed','tcp://'+proxy+':8080')
        # The proxy merely delays A's container creation, as a slow daemon could.
        # Both endpoints still report the identical Docker ID and share one slot.
        shared=prefix+'-shared:test';images.append(shared)
        a=build('a',delayed,shared,'echo A > /identity')
        for _ in range(120):
            with urllib.request.urlopen(proxybase+'/review/status') as r: state=r.read().decode()
            if state=='blocked':break
            time.sleep(1)
        else:raise RuntimeError('A did not reach container creation gate')
        b=build('b',local,shared,'echo B > /identity')
        wait(b)
        with urllib.request.urlopen(proxybase+'/review/release') as r:r.read()
        wait(a)
        aid=docker('ps','-q','--filter','label=com.docker.compose.project='+prefix+'-a')
        identity=docker('exec',aid,'cat','/identity')
        print(json.dumps({'probe':'shared-tag-race','maxConcurrentBuilds':1,'expectedA':'A','actualA':identity,'passed':identity=='A'}),flush=True)

        assert identity=='A', 'stack A deployed another build image'
        # A's job tag may be its last image reference after B moves the shared tag.
        # Never force-delete that image while A is using it.
        operator=api('POST','/users',{'Username':'operator','Password':password,'Role':2})['Id']
        api('PUT',f'/endpoints/{local}',{'UserAccessPolicies':{str(operator):{'RoleId':5,'StandaloneRole':'operator'}}})
        admin_token=token
        operator_token=api('POST','/auth',{'Username':'operator','Password':password})['jwt']
        image=prefix+'-operator:test';images.append(image)
        current=build('long',local,image,'sleep 15 && echo done > /identity')
        for _ in range(60):
            if 'sleep 15' in docker('top',builder):break
            time.sleep(0.25)
        else:raise RuntimeError('build did not reach RUN sleep')
        token=operator_token
        raised_memory=info['MemTotal']*60//100
        api('POST',f'/endpoints/{local}/docker/containers/{builder}/update',{'CpuQuota':-1,'Memory':raised_memory,'MemorySwap':raised_memory},expected=403)
        api('POST',f'/endpoints/{local}/docker/containers/{builder}/exec',{'Cmd':['true']},expected=403)
        observed=json.loads(docker('inspect','--format','{{json .HostConfig}}',builder))
        assert observed['CpuQuota']==info['NCPU']*50000 and observed['Memory']<=info['MemTotal']/2
        token=admin_token
        wait(current)
        print(json.dumps({'probe':'operator-modifies-active-builder','passed':True}),flush=True)

        # A genuinely separate daemon configured with host cgroup namespaces.
        # Outer test container limits remain in force to bound this probe itself.
        docker('run','-d','--name',dind,'--network',net,'--privileged','--cpus','2','--memory','2g','--memory-swap','2g','-e','DOCKER_TLS_CERTDIR=','docker:28.5.2-dind','--default-cgroupns-mode=host','--tls=false')
        for _ in range(90):
            p=subprocess.run(['docker','exec',dind,'docker','info','--format','{{json .}}'],capture_output=True,text=True)
            if p.returncode==0:
                inner=json.loads(p.stdout);break
            time.sleep(1)
        else:raise RuntimeError('nested daemon failed readiness')
        remote=endpoint('host-cgroup-namespace','tcp://'+dind+':2375')
        api('PUT','/settings',{'ComposeBuildPolicy':{'CPUPercent':1,'MemoryPercent':1,'MaxConcurrentBuilds':1}})
        code='import time,json; start=time.time(); cpu=time.process_time(); exec("while time.time()-start<3: pass"); json.dump({"wall":time.time()-start,"cpu":time.process_time()-cpu,"cgroup":open("/proc/self/cgroup").read()},open("/timing.json","w"))'
        image=prefix+'-hostns:test'
        sid=build('hostns',remote,image,"python -c '"+code+"'",'python:3.13-alpine')
        for _ in range(90):
            result=api('GET',f'/stacks/{sid}')
            if result['Status']!=3:break
            time.sleep(1)
        assert result['Status']==4,result
        assert 'private default cgroup namespace' in json.dumps(result.get('DeploymentStatus',[])),result
        assert not docker('exec',dind,'docker','ps','-aq'), 'unsafe builder was created'
        print(json.dumps({'probe':'host-cgroup-namespace','passed':True,'result':'rejected before builder creation'}),flush=True)
        policy={'CPUPercent':1,'MemoryPercent':1,'MaxConcurrentBuilds':1,'AutoConfigureBuilderCgroup':True}
        api('PUT','/settings',{'ComposeBuildPolicy':policy})
        assert api('GET','/settings')['ComposeBuildPolicy']['AutoConfigureBuilderCgroup'] is True
        sid=build('private',remote,image,"python -c '"+code+"'",'python:3.13-alpine')
        wait(sid)
        innername='buildx_buildkit_portainer-compose-'+hashlib.sha256(inner['ID'].encode()).hexdigest()[:20]
        config=json.loads(docker('exec',dind,'docker','inspect',innername))[0]
        assert config['HostConfig']['CgroupnsMode']=='private'
        assert config['HostConfig']['CpuQuota']==inner['NCPU']*1000
        assert config['HostConfig']['Memory']==inner['MemTotal']//100
        measured=json.loads(docker('exec',dind,'docker','run','--rm',image,'cat','/timing.json'))
        cores=measured['cpu']/measured['wall'];budget=inner['NCPU']/100
        assert cores<=budget*1.3+0.05,(cores,budget)
        policy['AutoConfigureBuilderCgroup']=False
        api('PUT','/settings',{'ComposeBuildPolicy':policy})
        reused=build('private-reuse',remote,image,'true','python:3.13-alpine')
        wait(reused)
        assert json.loads(docker('exec',dind,'docker','inspect',innername))[0]['Id']==config['Id']
        after=json.loads(docker('exec',dind,'docker','info','--format','{{json .}}'))
        assert 'name=cgroupns' not in after['SecurityOptions'], 'daemon default was changed'
        print(json.dumps({'probe':'opt-in-private-builder','passed':True,'cpuBudget':budget,'measuredCPU':cores,'daemonDefaultUnchanged':True,'reusedAfterSwitchOff':True}),flush=True)
    except Exception:
        print(docker('logs','--tail','60',server),flush=True)
        raise
    finally:
        subprocess.run(['docker','rm','-fv',server,proxy,dind,builder],capture_output=True)
        for name in stacks:
            ids=docker('ps','-aq','--filter','label=com.docker.compose.project='+name).splitlines()
            for cid in ids:
                image_id=docker('inspect','--format','{{.Image}}',cid)
                details=json.loads(docker('image','inspect',image_id))[0]
                images.extend(tag for tag in (details.get('RepoTags') or []) if tag.startswith('portainer-build-job:'))
            if ids:subprocess.run(['docker','rm','-fv',*ids],capture_output=True)
            nets=docker('network','ls','-q','--filter','label=com.docker.compose.project='+name).splitlines()
            if nets:subprocess.run(['docker','network','rm',*nets],capture_output=True)
        subprocess.run(['docker','volume','rm',data,builder+'_state'],capture_output=True)
        subprocess.run(['docker','network','rm',net],capture_output=True)
        if images:subprocess.run(['docker','image','rm',*images],capture_output=True)

if __name__=='__main__':main()
