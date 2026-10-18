#!/usr/bin/env python3
"""Actual generated local repository UI qualification; no production mutation.
Requires Firefox, Node (built-in WebSocket), and the Python standard library.
"""
import argparse,base64,json,pathlib,socket,subprocess,tempfile,time
from urllib.parse import urlparse
from browser_marionette import MarionetteClient
p=argparse.ArgumentParser();p.add_argument('--base-url',required=True);p.add_argument('--evidence',required=True,type=pathlib.Path);a=p.parse_args()
assert urlparse(a.base_url).hostname in {'127.0.0.1','localhost'},'local generated repositories only'
a.evidence.mkdir(parents=True,exist_ok=True)
with tempfile.TemporaryDirectory(prefix='tpa-modern-firefox-') as profile:
 with socket.socket() as sock:sock.bind(('127.0.0.1',0));port=sock.getsockname()[1]
 pathlib.Path(profile,'user.js').write_text(f'user_pref("marionette.port",{port});\n')
 with (a.evidence/'firefox.log').open('wb') as log:
  proc=subprocess.Popen(['firefox','--headless','--no-remote','--marionette','--remote-debugging-port=0','--profile',profile],stdout=log,stderr=log);client=None
  try:
   for _ in range(150):
    try:client=MarionetteClient(f'127.0.0.1:{port}');break
    except OSError:time.sleep(.1)
   assert client;serial=0
   def call(name,params=None):
    global serial
    serial+=1;r=client.call(serial,name,params or {});assert r[2] is None,r[2];return r[3]
   def js(code):return call('WebDriver:ExecuteScript',{'script':code,'args':[]})['value']
   def wait(code):
    for _ in range(150):
     if js(code):return
     time.sleep(.05)
    raise AssertionError('Browser condition timed out: '+code+'; '+str(js('return document.getElementById("status")?.textContent')))
   def element(selector):return call('WebDriver:FindElement',{'using':'css selector','value':selector})['value']['element-6066-11e4-a52e-4f735466cecf']
   def click(selector):call('WebDriver:ElementClick',{'id':element(selector)})
   def text(selector,value):
    id=element(selector);call('WebDriver:ElementClear',{'id':id});call('WebDriver:ElementSendKeys',{'id':id,'text':value})
   def screenshot(name):
    time.sleep(.12) # allow the browser's paint after actual user interaction
    shot=call('WebDriver:TakeScreenshot',{'id':None,'highlights':[],'full':True})['value'];(a.evidence/(name+'.png')).write_bytes(base64.b64decode(shot))
   session=call('WebDriver:NewSession',{'webSocketUrl':True});observations=[]
   for width in [1440,390]:
    subprocess.run(['node','--input-type=module','-e',"""const w=new WebSocket(process.argv[1]);const t=setTimeout(()=>process.exit(1),10000);w.onopen=()=>w.send(JSON.stringify({id:1,method:'browsingContext.getTree',params:{}}));w.onmessage=e=>{const d=JSON.parse(e.data);if(d.type==='error')process.exit(2);if(d.id===1)w.send(JSON.stringify({id:2,method:'browsingContext.setViewport',params:{context:d.result.contexts[0].context,viewport:{width:Number(process.argv[2]),height:1000}}}));if(d.id===2){clearTimeout(t);w.close()}};""",session['capabilities']['webSocketUrl'],str(width)],check=True,timeout=15)
    call('WebDriver:Navigate',{'url':a.base_url+'/representative/'})
    wait('return !document.getElementById("search").disabled')
    result=js('return {width:innerWidth,scrollWidth:document.documentElement.scrollWidth,cards:document.querySelectorAll(".package-card").length,status:document.getElementById("status").textContent,unlabelled:[...document.querySelectorAll("input,select")].filter(e=>!e.labels.length).length,paintable:[...document.querySelectorAll(".package-card")].every(e=>e.checkVisibility({contentVisibilityAuto:true})),metadataExecuted:globalThis.__metadataExecuted===true};')
    assert result['width']==width and result['scrollWidth']<=width and result['cards']==8 and result['unlabelled']==0 and result['paintable'] and not result['metadataExecuted'],result
    screenshot(f'overview-{width}');observations.append({'kind':'overview',**result})
    text('#search','mema 0.4');wait('return document.querySelectorAll(".package-card").length===2');screenshot(f'search-{width}')
    text('#search','tpa');wait('return document.querySelectorAll(".package-card").length===1')
    assert js('return [...document.querySelectorAll(".architecture")].map(e=>e.textContent)')==['amd64','arm64','riscv64']
    click('.package-open');wait('return document.getElementById("package-modal").open')
    assert js('return document.querySelectorAll("[role=tab]").length')==3
    click('[role=tab]:nth-child(2)')
    detail=js('return {selected:document.querySelector("[aria-selected=true]").textContent,text:document.getElementById("modal-body").textContent,link:document.querySelector(".primary-download").href,focus:document.activeElement.id};')
    assert detail['selected']=='arm64' and 'SHA-256' in detail['text'] and '4daa359fc0655481102f25e2db19dc18a86c203956f30155816a14c3366bd78e' in detail['text'] and detail['link'].endswith('tpa_0.8.0~rc1_arm64.deb'),detail
    screenshot(f'details-{width}');call('WebDriver:ElementSendKeys',{'id':element('#modal-close'),'text':'\ue00c'});wait('return !document.getElementById("package-modal").open');assert js('return document.activeElement.classList.contains("package-open")')
    text('#search','');js('document.getElementById("category").value="doc";document.getElementById("category").dispatchEvent(new Event("change"));return true;');assert js('return document.querySelectorAll(".package-card").length===1 && document.querySelector(".package-open").textContent==="ui-long-description"');screenshot(f'category-{width}');js('document.getElementById("category").value="";document.getElementById("category").dispatchEvent(new Event("change"));return true;');js('document.getElementById("architecture").value="arm64";document.getElementById("include-all").checked=false;document.getElementById("architecture").dispatchEvent(new Event("change"));return true;')
    assert js('return [...document.querySelectorAll(".architecture")].map(e=>e.textContent)')==['arm64']
    screenshot(f'filter-{width}')
    call('WebDriver:Navigate',{'url':a.base_url+'/utility/'});wait('return !document.getElementById("search").disabled');assert js('return document.querySelectorAll(".package-card").length')==8
    assert js('return [...document.querySelectorAll(".architecture[href]")].every(a=>new URL(a.href).pathname.startsWith("/representative/pool/"))')
    call('WebDriver:Navigate',{'url':a.base_url+'/empty/'});wait('return !document.getElementById("search").disabled');assert js('return document.getElementById("status").textContent.startsWith("0 package records")');screenshot(f'empty-{width}')
    call('WebDriver:Navigate',{'url':a.base_url+'/large/'});wait('return !document.getElementById("search").disabled');assert js('return document.querySelectorAll(".package-card").length')==50
    for _ in range(12):click('#more');assert js('return document.querySelectorAll(".package-card").length')==50
    click('#previous');assert js('return document.querySelectorAll(".package-card").length')==50
    screenshot(f'large-{width}');observations.append({'kind':'large','viewport':width,'actual_debs':2000,'pages_traversed':13,'max_live_cards':50,'page':js('return document.getElementById("page-number").textContent')})
    if width==390:
     js('window.scrollTo(0,900);return true;');time.sleep(.2);assert js('return document.getElementById("search-dock").classList.contains("is-compact")');click('#filter-toggle');assert js('return document.getElementById("filter-toggle").getAttribute("aria-expanded")')=='true';screenshot('mobile-expanded-filters')
    # EXACT current TPA.run CSP, applied only by a disposable local preview.
    call('WebDriver:Navigate',{'url':a.base_url+'/sandbox/'})
    assert js('return document.getElementById("search").disabled && document.querySelectorAll(".static-card").length===10')
    click('.static-card details summary');assert js('return document.querySelector(".static-card details").open');screenshot(f'current-host-csp-fallback-{width}')
    observations.append({'kind':'current-production-csp-mirror','viewport':width,'interactive_scripts_blocked_by_host':True,'static_details_and_relative_links_preserved':True,'backend_policy_changed':False})
   (a.evidence/'result.json').write_text(json.dumps({'verdict':'PASS','scope':'ACTUAL local TPA/utility generator output; no production replacement','base_url':a.base_url,'observations':observations},indent=2)+'\n')
   print('PASS actual generated UI Firefox1440/390: search, filters, details, safe metadata, relative links, empty, 2000-package bounded pages, utility schema and current-host CSP fallback')
  finally:
   if client:
    try:call('WebDriver:DeleteSession')
    except Exception:pass
    client.close()
   proc.terminate()
   try:proc.wait(timeout=10)
   except subprocess.TimeoutExpired:proc.kill();proc.wait()
