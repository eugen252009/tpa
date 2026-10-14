package tpautils_test

import (
	"os/exec"
	"testing"
)

// A dependency-free DOM mock exercises the shipped JS, not a copy of its logic.
// This is not a visual/headless-browser rendering qualification.
func TestBrowserJavaScript(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("optional Node JS logic oracle unavailable")
	}
	const script = `
const vm=require("node:vm"), fs=require("node:fs"), assert=require("node:assert/strict");
const code=fs.readFileSync(process.argv[1],"utf8");
class Element {
 constructor(){this.children=[];this.listeners={};this.value="";this.textContent="";this.hidden=false;}
 append(...nodes){this.children.push(...nodes);}
 replaceChildren(){this.children=[];}
 addEventListener(name,fn){this.listeners[name]=fn;}
 set innerHTML(_){throw new Error("Unsafe HTML rendering");}
}
function pkg(name,architecture="all",filename="pool/main/a/aa/aa_1.0_all.deb"){
 return {package:name,architecture,version:"1.0",filename,size:512,sha256:"abc",control:{Description:"<script>not markup</script>"}};
}
async function load(packages,base="https://repo.invalid/artifacts/",version=1){
 const elements={};for(const id of ["status","packages","search","architecture","more"])elements[id]=new Element();
 const data={format:"tpa-utils-repository-browser",version,base_url:base,repository:{packages,signatureVerified:false}};
 const document={getElementById:id=>elements[id],createElement:()=>new Element(),baseURI:"https://repo.invalid/browser/"};
 await vm.runInNewContext(code,{document,URL,fetch:async()=>({ok:true,json:async()=>data})});
 return elements;
}
(async()=>{
 const first=pkg("safe+package");first.control.Depends="libc6 (>= 2.36)";
 const e=await load([first,pkg("arm-package","arm64"),pkg("bad-link","all","pool/../evil")]);
 assert.equal(e.packages.children.length,3);
 assert.equal(e.packages.children[0].children[2].href,"https://repo.invalid/artifacts/pool/main/a/aa/aa_1.0_all.deb");
 assert.match(e.packages.children[0].children[1].textContent,/<script>not markup<\/script>/);
 assert.equal(e.packages.children[2].children.length,2); // traversal has no link
 e.search.value="libc6";e.search.listeners.input();assert.equal(e.packages.children.length,1);
 e.search.value="";e.architecture.value="arm64";e.architecture.listeners.change();assert.equal(e.packages.children.length,1);
 assert.match(e.packages.children[0].children[0].textContent,/arm-package/);
 const many=await load(Array.from({length:300},(_,i)=>pkg("package-"+i)));
 assert.equal(many.packages.children.length,200);assert.equal(many.more.hidden,false);
 many.more.listeners.click();assert.equal(many.packages.children.length,300);assert.equal(many.more.hidden,true);
 const empty=await load([]);assert.match(empty.status.textContent,/0 of 0/);assert.equal(empty.more.hidden,true);
 const bad=await load([],"javascript:alert(1)");assert.match(bad.status.textContent,/Unsafe repository URL/);
 const unsupported=await load([],"https://repo.invalid/",2);assert.match(unsupported.status.textContent,/Unsupported inventory schema/);
})().catch(err=>{console.error(err);process.exitCode=1;});
`
	cmd := exec.Command(node, "-e", script, "web/app.js")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("shipped JS behavior: %v\n%s", err, output)
	}
}
