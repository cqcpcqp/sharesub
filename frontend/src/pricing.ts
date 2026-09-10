export interface TokenPrices { input:number; output:number; cache_read:number; cache_write:number; image_input:number; image_output:number }
export interface ModelPrice { model:string; standard:TokenPrices; long_context_tokens:number; long_input_bps:number; long_output_bps:number }
export interface PricingConfig { models:ModelPrice[]; fast_multiplier_bps:number; flex_multiplier_bps:number; web_search_micros:number; image_1k_micros:number; image_2k_micros:number; image_4k_micros:number }
export interface PricingVersionSummary { id:number; published_at:string; published_by:string; reason:string }
export interface PricingVersion extends PricingVersionSummary { config:PricingConfig }
export interface PublishPricingInput { base_version_id:number; reason:string; config:PricingConfig }
export type PricingTier = 'standard'
export const pricingTiers = [{ value:'standard' as const, label:'Standard' }]
export const tokenPriceFields:{key:keyof TokenPrices;label:string}[] = [
 {key:'input',label:'输入'},{key:'cache_read',label:'缓存读取'},{key:'cache_write',label:'缓存写入'},{key:'output',label:'输出'},{key:'image_input',label:'图片输入 Token'},{key:'image_output',label:'图片输出 Token'},
]
export const addonPriceFields:{key:Exclude<keyof PricingConfig,'models'|'fast_multiplier_bps'|'flex_multiplier_bps'>;label:string;unit:string}[] = [
 {key:'web_search_micros',label:'Web Search',unit:'USD / 次'},{key:'image_1k_micros',label:'图片 1K',unit:'USD / 张'},{key:'image_2k_micros',label:'图片 2K',unit:'USD / 张'},{key:'image_4k_micros',label:'图片 4K',unit:'USD / 张'},
]
export interface PriceChange {item:string;field:string;before:number;after:number;unit:string;scale:number}
export function pricingChanges(before:PricingConfig,after:PricingConfig):PriceChange[] { const out:PriceChange[]=[]; const add=(item:string,field:string,a:number,b:number,unit:string,scale:number)=>{if(a!==b)out.push({item,field,before:a,after:b,unit,scale})}; add('全局档位','Fast / Priority 倍率',before.fast_multiplier_bps,after.fast_multiplier_bps,'倍',10000); add('全局档位','Flex 倍率',before.flex_multiplier_bps,after.flex_multiplier_bps,'倍',10000); for(const model of after.models){const old=before.models.find(x=>x.model===model.model)!;for(const f of tokenPriceFields)add(model.model,f.label,old.standard[f.key],model.standard[f.key],'USD / 1M Token',1000000);add(model.model,'长上下文阈值',old.long_context_tokens,model.long_context_tokens,'Token',1);add(model.model,'长上下文输入 / 缓存倍率',old.long_input_bps,model.long_input_bps,'倍',10000);add(model.model,'长上下文输出倍率',old.long_output_bps,model.long_output_bps,'倍',10000)} for(const f of addonPriceFields)add('附加计价',f.label,before[f.key],after[f.key],f.unit,1000000);return out }
export function formatPrice(value:number,scale=1000000){return new Intl.NumberFormat('en-US',{maximumFractionDigits:6}).format(value/scale)}
export function priceChangePercent(c:PriceChange){if(c.before===0)return'由零价调整';const p=(c.after-c.before)/c.before*100;return`${p>0?'+':''}${p.toFixed(1)}%`}
export function exampleCost(model:ModelPrice,_tier:PricingTier='standard',input=100000,cached=50000,output=10000){const long=model.long_context_tokens>0&&input>model.long_context_tokens;const factor=long?model.long_input_bps:10000;const ofactor=long?model.long_output_bps:10000;const round=(t:number,p:number,f:number)=>Number((BigInt(t)*BigInt(p)*BigInt(f)+5000000000n)/10000000000n);return round(input-cached,model.standard.input,factor)+round(cached,model.standard.cache_read,factor)+round(output,model.standard.output,ofactor)}
