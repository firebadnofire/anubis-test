#include <cuda_runtime.h>
#include <cstdint>
#include <cstdio>
#include <cstring>
#include <limits>

// Fixed-width C ABI shared with the Windows Go client.
struct Job { char data[256]; uint64_t base; uint32_t length; uint32_t difficulty; };
struct Prepared { uint32_t state[8]; unsigned char tail[64]; uint64_t base; uint32_t length, tailLength, difficulty; };
struct Result { uint64_t nonce; unsigned char hash[32]; };
static_assert(sizeof(Job) == 272 && sizeof(Result) == 40, "ABI layout");
__device__ __constant__ uint32_t constants[64] = {
  0x428a2f98,0x71374491,0xb5c0fbcf,0xe9b5dba5,0x3956c25b,0x59f111f1,0x923f82a4,0xab1c5ed5,
  0xd807aa98,0x12835b01,0x243185be,0x550c7dc3,0x72be5d74,0x80deb1fe,0x9bdc06a7,0xc19bf174,
  0xe49b69c1,0xefbe4786,0x0fc19dc6,0x240ca1cc,0x2de92c6f,0x4a7484aa,0x5cb0a9dc,0x76f988da,
  0x983e5152,0xa831c66d,0xb00327c8,0xbf597fc7,0xc6e00bf3,0xd5a79147,0x06ca6351,0x14292967,
  0x27b70a85,0x2e1b2138,0x4d2c6dfc,0x53380d13,0x650a7354,0x766a0abb,0x81c2c92e,0x92722c85,
  0xa2bfe8a1,0xa81a664b,0xc24b8b70,0xc76c51a3,0xd192e819,0xd6990624,0xf40e3585,0x106aa070,
  0x19a4c116,0x1e376c08,0x2748774c,0x34b0bcb5,0x391c0cb3,0x4ed8aa4a,0x5b9cca4f,0x682e6ff3,
  0x748f82ee,0x78a5636f,0x84c87814,0x8cc70208,0x90befffa,0xa4506ceb,0xbef9a3f7,0xc67178f2
};
__device__ __forceinline__ uint32_t rotate(uint32_t x, int n) { return (x >> n) | (x << (32-n)); }
__device__ __forceinline__ void compress(uint32_t* s, const unsigned char* block) {
    uint32_t w[64];
    #pragma unroll
    for (int i=0;i<16;i++) w[i]=(uint32_t(block[4*i])<<24)|(uint32_t(block[4*i+1])<<16)|(uint32_t(block[4*i+2])<<8)|block[4*i+3];
    #pragma unroll
    for (int i=16;i<64;i++) {
        uint32_t a=w[i-15], b=w[i-2];
        w[i]=w[i-16]+(rotate(a,7)^rotate(a,18)^(a>>3))+w[i-7]+(rotate(b,17)^rotate(b,19)^(b>>10));
    }
    uint32_t a=s[0],b=s[1],c=s[2],d=s[3],e=s[4],f=s[5],g=s[6],h=s[7];
    #pragma unroll
    for (int i=0;i<64;i++) {
        uint32_t t=h+(rotate(e,6)^rotate(e,11)^rotate(e,25))+((e&f)^(~e&g))+constants[i]+w[i];
        uint32_t u=(rotate(a,2)^rotate(a,13)^rotate(a,22))+((a&b)^(a&c)^(b&c));
        h=g;g=f;f=e;e=d+t;d=c;c=b;b=a;a=t+u;
    }
    s[0]+=a;s[1]+=b;s[2]+=c;s[3]+=d;s[4]+=e;s[5]+=f;s[6]+=g;s[7]+=h;
}
__global__ void prepare(const Job* jobs, Prepared* prepared, int count) {
    int i=blockIdx.x*blockDim.x+threadIdx.x; if(i>=count) return;
    const Job& j=jobs[i]; Prepared& p=prepared[i];
    uint32_t init[8]={0x6a09e667,0xbb67ae85,0x3c6ef372,0xa54ff53a,0x510e527f,0x9b05688c,0x1f83d9ab,0x5be0cd19};
    for(int k=0;k<8;k++) p.state[k]=init[k];
    for(unsigned off=0;off+64<=j.length;off+=64) compress(p.state,reinterpret_cast<const unsigned char*>(j.data)+off);
    p.tailLength=j.length%64;
    for(unsigned k=0;k<p.tailLength;k++) p.tail[k]=j.data[j.length-p.tailLength+k];
    p.length=j.length; p.base=j.base;p.difficulty=j.difficulty;
}
__device__ __forceinline__ void hashNonce(const Prepared& p, uint64_t nonce, uint32_t* s) {
    unsigned char block[128]={}; char digits[20]; int n=0;
    do { digits[n++]=char('0'+nonce%10);nonce/=10; } while(nonce);
    for(unsigned k=0;k<p.tailLength;k++) block[k]=p.tail[k];
    for(int k=0;k<n;k++) block[p.tailLength+k]=digits[n-1-k];
    unsigned length=p.tailLength+n; block[length]=0x80;
    unsigned blocks=length<56?1:2;
    uint64_t bits=uint64_t(p.length+n)*8;
    for(int k=0;k<8;k++) block[blocks*64-1-k]=unsigned char((bits>>(k*8))&255);
    for(int k=0;k<8;k++) s[k]=p.state[k];
    compress(s,block); if(blocks==2) compress(s,block+64);
}
__global__ void search(const Prepared* jobs, Result* results, unsigned count, unsigned attempts) {
    unsigned i=blockIdx.x*blockDim.x+threadIdx.x;
    if(i>=count*attempts) return;
    unsigned job=i/attempts, offset=i%attempts;
    uint64_t nonce=jobs[job].base+offset;
    uint32_t s[8]; hashNonce(jobs[job],nonce,s);
    unsigned bits=jobs[job].difficulty*4;
    // Benchmark supports 0..8 nibbles; avoid shifting by 32.
    bool valid=bits==0 || (bits==32?s[0]==0:(s[0]>>(32-bits))==0);
    if(valid) atomicMin(reinterpret_cast<unsigned long long*>(&results[job].nonce),static_cast<unsigned long long>(nonce));
}
__global__ void finish(const Prepared* jobs,Result* results,int count) {
    int i=blockIdx.x*blockDim.x+threadIdx.x;if(i>=count || results[i].nonce==UINT64_MAX) return;
    uint32_t s[8];hashNonce(jobs[i],results[i].nonce,s);
    for(int k=0;k<32;k++) results[i].hash[k]=(s[k/4]>>(24-8*(k%4)))&255;
}
static Job* deviceJobs=nullptr; static Prepared* devicePrepared=nullptr; static Result* deviceResults=nullptr;
static char errorMessage[256]={};
static int check(cudaError_t status) {
    if(status==cudaSuccess) return 0;
    std::snprintf(errorMessage,sizeof(errorMessage),"%s",cudaGetErrorString(status));return 1;
}
#define CHECK(call) do { if(check(call)) return 1; } while(0)
extern "C" __declspec(dllexport) int GPUError(char* output,unsigned capacity) {
    if(!output || capacity<256) return 1;
    std::memcpy(output,errorMessage,256); return 0;
}
extern "C" __declspec(dllexport) int GPUInit() {
    CHECK(cudaSetDevice(0));
    cudaDeviceProp prop{}; CHECK(cudaGetDeviceProperties(&prop,0));
    if(prop.major!=8 || prop.minor!=9) { std::snprintf(errorMessage,sizeof(errorMessage),"Expected Ada GPU (compute 8.9), got %s",prop.name);return 1; }
    CHECK(cudaMalloc(&deviceJobs,32*sizeof(Job)));
    CHECK(cudaMalloc(&devicePrepared,32*sizeof(Prepared)));
    CHECK(cudaMalloc(&deviceResults,32*sizeof(Result)));return 0;
}
extern "C" __declspec(dllexport) int GPUBatch(const Job* jobs,unsigned count,unsigned attempts,Result* results) {
    if(!deviceJobs || count<1 || count>32 || attempts<1 || attempts>262144) { std::snprintf(errorMessage,sizeof(errorMessage),"Invalid batch bounds");return 1; }
    for(unsigned i=0;i<count;i++) if(jobs[i].length>256 || jobs[i].difficulty>8 || jobs[i].base>INT64_MAX-attempts) { std::snprintf(errorMessage,sizeof(errorMessage),"Invalid job bounds");return 1; }
    CHECK(cudaMemcpy(deviceJobs,jobs,count*sizeof(Job),cudaMemcpyHostToDevice));
    CHECK(cudaMemset(deviceResults,255,count*sizeof(Result)));
    prepare<<<1,32>>>(deviceJobs,devicePrepared,count);CHECK(cudaGetLastError());
    search<<<(count*attempts+255)/256,256>>>(devicePrepared,deviceResults,count,attempts);CHECK(cudaGetLastError());
    finish<<<1,32>>>(devicePrepared,deviceResults,count);CHECK(cudaGetLastError());
    CHECK(cudaDeviceSynchronize());
    CHECK(cudaMemcpy(results,deviceResults,count*sizeof(Result),cudaMemcpyDeviceToHost));return 0;
}
extern "C" __declspec(dllexport) int GPUClose() {
    CHECK(cudaFree(deviceJobs));CHECK(cudaFree(devicePrepared));CHECK(cudaFree(deviceResults));
    deviceJobs=nullptr;devicePrepared=nullptr;deviceResults=nullptr;return 0;
}
