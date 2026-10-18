package repositoryweb_test

import (
	"github.com/eugen252009/tpa/pkg/repositoryweb"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// Executes the ONE shipped asset with a hostile DOM oracle. Visual/layout and
// genuine browser behavior are separately qualified against generated output.
func TestSharedJavaScript(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("Node unavailable")
	}
	asset, err := repositoryweb.Asset("app.js")
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "app.js")
	if err := os.WriteFile(p, asset, 0600); err != nil {
		t.Fatal(err)
	}
	const script = `
const vm=require('node:vm'),fs=require('node:fs'),assert=require('node:assert/strict');
const code=fs.readFileSync(process.argv[1],'utf8');
class Element {
 constructor(tag='div'){this.tag=tag;this.children=[];this.listeners={};this.value='';this.hidden=false;this.disabled=false;this.checked=true;this.attributes={};this.textContent='';}
 append(...nodes){this.children.push(...nodes);} replaceChildren(...nodes){this.children=nodes;}
 addEventListener(name,fn){this.listeners[name]=fn;} setAttribute(k,v){this.attributes[k]=v;}
 focus(){this.focused=true;} showModal(){this.open=true;} close(){this.open=false;}
 set innerHTML(_){throw Error('unsafe HTML rendering');}
}
function pkg(name,architecture='all',version='1.0',filename='pool/main/a/'+name+'_'+version+'_'+architecture+'.deb'){
 return {package:name,architecture,version,filename,size:512,sha256:'abc',control:{Description:'<script>not markup</script>',Depends:'libfixture (>= 1)',Section:'utils'}};
}
function flatten(node){return [node,...node.children.flatMap(flatten)];}
async function load(packages,canonical=false,base='https://repo.invalid/artifacts/',version=1){
 const elements={};for(const id of ['packages','status','search','architecture','category','include-all','package-modal','modal-title','modal-body','filters','more','previous','matches','pagination','page-number','modal-close','repository-link','filter-toggle','trust-warning'])elements[id]=new Element();
 const data=canonical?{format:'tpa-repository-index',version,packages:packages.map(p=>({metadata:{...p.control,Package:p.package,Version:p.version,Architecture:p.architecture},artifact:{filename:p.filename,size:p.size,sha256:p.sha256}}))}:{format:'tpa-utils-repository-browser',version,base_url:base,repository:{packages,signed:false,signatureVerified:false}};
 const document={getElementById:id=>elements[id],createElement:tag=>new Element(tag),baseURI:'https://repo.invalid/browser/'};
 await vm.runInNewContext(code,{document,URL,fetch:async()=>({ok:true,json:async()=>data})});return elements;
}
(async()=>{
 for(const canonical of [false,true]){
  const e=await load([pkg('alpha','amd64'),pkg('alpha','arm64'),pkg('common'),pkg('other','arm64'),pkg('alpha','amd64')],canonical);
  assert.equal(e.packages.children.length,3);assert.match(e.status.textContent,/4 package records/);
  assert(flatten(e.packages).some(n=>n.textContent==='<script>not markup</script>'));
  const links=flatten(e.packages).filter(n=>n.tag==='a');assert(links[0].href.startsWith(canonical?'https://repo.invalid/browser/pool/':'https://repo.invalid/artifacts/pool/'));
  e.search.value='libfixture';e.search.listeners.input();assert.equal(e.packages.children.length,3);
  e.architecture.value='amd64';e.architecture.listeners.change();assert.equal(e.packages.children.length,2);
  e['include-all'].checked=false;e['include-all'].listeners.change();assert.equal(e.packages.children.length,1);
  e.category.value='missing';e.category.listeners.change();assert.equal(e.packages.children.length,0);
  e.category.value='';e.category.listeners.change();
  flatten(e.packages).find(n=>n.className==='package-open').listeners.click();assert.equal(e['package-modal'].open,true);
  const tabs=flatten(e['modal-body']).filter(n=>n.className==='architecture-tab');assert.equal(tabs.length,2);
  tabs[0].listeners.keydown({key:'ArrowRight',preventDefault(){}});assert.equal(tabs[1].attributes['aria-selected'],'true');
  assert(flatten(e['modal-body']).some(n=>n.textContent==='SHA-256'));assert(flatten(e['modal-body']).some(n=>n.textContent==='abc'));
  e['modal-close'].listeners.click();assert.equal(e['package-modal'].open,false);
 }
 const many=await load(Array.from({length:503},(_,i)=>pkg('package-'+i)),true);
 assert.equal(many.packages.children.length,50);
 for(let i=0;i<10;i++){many.more.listeners.click();assert(many.packages.children.length<=50);}
 assert.equal(many.packages.children.length,3);assert(many.more.disabled);
 many.previous.listeners.click();assert.equal(many.packages.children.length,50);
 many.search.value='no matches';many.search.listeners.input();assert.equal(many.packages.children.length,0);
 const bad=await load([pkg('bad','all','1','pool/../evil')]);assert.equal(flatten(bad.packages).filter(n=>n.tag==='a').length,0);
 const encoded=await load([pkg('encoded','all','1','pool/a?b#c%.deb')]);assert(flatten(encoded.packages).find(n=>n.tag==='a').href.endsWith('pool/a%3Fb%23c%25.deb'));
 const conflict=pkg('same');const changed={...conflict,filename:'pool/other.deb'};assert.match((await load([conflict,changed],true)).status.textContent,/Conflicting package identity/);
 const changedMetadata={...conflict,control:{...conflict.control,Description:'different'}};assert.match((await load([conflict,changedMetadata],true)).status.textContent,/Conflicting package identity/);
 assert.match((await load([])).status.textContent,/0 package records/);
 assert.match((await load([],false,'javascript:alert(1)')).status.textContent,/Unsafe repository URL/);
 assert.match((await load([],false,'https://repo.invalid/',2)).status.textContent,/Unsupported inventory schema/);
})().catch(error=>{console.error(error);process.exitCode=1;});
`
	if out, err := exec.Command(node, "-e", script, p).CombinedOutput(); err != nil {
		t.Fatalf("shared shipped JS: %v\n%s", err, out)
	}
}
