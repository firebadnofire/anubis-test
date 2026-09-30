#include <cuda_runtime.h>
#include <cstdio>

__global__ void probe(int* result) { *result = 4090; }
int main() {
    cudaDeviceProp props{};
    int* device = nullptr;
    int result = 0;
    auto check = [](cudaError_t status) {
        if (status != cudaSuccess) {
            std::fprintf(stderr, "CUDA probe failed: %s\n", cudaGetErrorString(status));
            return false;
        }
        return true;
    };
    if (!check(cudaGetDeviceProperties(&props, 0)) || !check(cudaMalloc(&device, sizeof(int)))) return 1;
    probe<<<1, 1>>>(device);
    if (!check(cudaGetLastError()) || !check(cudaDeviceSynchronize()) ||
        !check(cudaMemcpy(&result, device, sizeof(int), cudaMemcpyDeviceToHost))) return 1;
    if (!check(cudaFree(device)) || result != 4090) return 1;
    std::printf("CUDA execution verified: %s (compute %d.%d)\n", props.name, props.major, props.minor);
    return 0;
}
