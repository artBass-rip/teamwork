import test from 'node:test';
import assert from 'node:assert/strict';
import {LlmAnalyzer} from '../src/llm.mjs';

test('Ollama semantic response becomes issue-key based analyzer rules', async()=>{
  const fetchImpl=async()=>({ok:true,json:async()=>({message:{content:JSON.stringify({groups:[{name:'CI/CD runners',description:'Runner scheduling',examples:['Refactor runner scheduler'],issueKeys:['DO-1']}]})}})});
  const analyzer=new LlmAnalyzer({settings:{provider:'ollama',ollamaUrl:'http://ollama',ollamaModel:'test'},fetchImpl});
  const groups=await analyzer.analyze({key:'DO',name:'DevOps'},[{key:'DO-1',title:'[DEVOPS] Runners: Refactor scheduler aqa runner by aws',labels:[],components:[]}]);
  assert.deepEqual(groups,[{name:'CI/CD runners',description:'Runner scheduling',examples:['Refactor runner scheduler'],issueKeys:['DO-1'],source:'analyzer'}]);
});

test('connecting external LLM does not change provider selection',()=>{
  const settings={provider:'ollama',external:{baseUrl:'https://api.example/v1',model:'semantic'}};
  assert.equal(settings.provider,'ollama');
});

test('large projects are analyzed in bounded batches',async()=>{
  let calls=0;
  const fetchImpl=async(_url,request)=>{
    calls+=1;
    const keys=JSON.parse(request.body).messages[1].content.match(/DO-\d+/g);
    return {ok:true,json:async()=>({message:{content:JSON.stringify({groups:[{name:'Platform',issueKeys:keys}]})}})};
  };
  const issues=Array.from({length:51},(_,index)=>({key:`DO-${index+1}`,title:`Work ${index+1}`}));
  const groups=await new LlmAnalyzer({settings:{provider:'ollama'},fetchImpl}).analyze({key:'DO',name:'DevOps'},issues);
  assert.equal(calls,3);
  assert.equal(groups[0].issueKeys.length,51);
});
