// Copyright 2025 The GoGPU Authors
// SPDX-License-Identifier: MIT

//go:build darwin && !(js && wasm)

package metal_test

import (
	"context"
	"encoding/binary"
	"math"
	"testing"

	"github.com/gogpu/gputypes"
	"github.com/gogpu/wgpu"
	_ "github.com/gogpu/wgpu/hal/metal"
)

const quadShader = `
@vertex
fn vs_main(@location(0) pos: vec2<f32>) -> @builtin(position) vec4<f32> {
    return vec4<f32>(pos, 0.0, 1.0);
}
@fragment
fn fs_main() -> @location(0) vec4<f32> {
    return vec4<f32>(0.0, 1.0, 0.0, 1.0);
}
`

// TestMetalDrawIndexedOffsets draws a full-target quad behind a degenerate
// prefix of indices and vertices that FirstIndex and BaseVertex skip.
// drawIndexedPrimitives takes ten objc_msgSend arguments, so BaseVertex and
// baseInstance travel on the stack: a wrong value there draws nothing.
func TestMetalDrawIndexedOffsets(t *testing.T) {
	for _, tc := range []struct {
		name       string
		firstIndex uint32
		baseVertex int32
	}{{"plain", 0, 0}, {"offsets", 6, 4}} {
		t.Run(tc.name, func(t *testing.T) {
			if got := drawQuad(t, tc.firstIndex, tc.baseVertex); got < 15*16 {
				t.Fatalf("%d/256 pixels filled", got)
			}
		})
	}
}

func drawQuad(t *testing.T, firstIndex uint32, baseVertex int32) int {
	instance, err := wgpu.CreateInstance(&wgpu.InstanceDescriptor{Backends: wgpu.BackendsPrimary})
	if err != nil {
		t.Skipf("CreateInstance: %v", err)
	}
	defer instance.Release()
	adapter, err := instance.RequestAdapter(nil)
	if err != nil {
		t.Skipf("RequestAdapter: %v", err)
	}
	defer adapter.Release()
	if b := adapter.Info().Backend; b != gputypes.BackendMetal {
		t.Skipf("adapter backend %v", b)
	}
	device, err := adapter.RequestDevice(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer device.Release()
	q := device.Queue()

	var verts []float32
	for range baseVertex {
		verts = append(verts, 0.5, 0.5)
	}
	verts = append(verts, -1, -1, 1, -1, 1, 1, -1, 1)
	vbytes := make([]byte, 4*len(verts))
	for i, f := range verts {
		binary.LittleEndian.PutUint32(vbytes[4*i:], math.Float32bits(f))
	}
	indices := append(make([]uint16, firstIndex), 0, 1, 2, 2, 3, 0)
	ibytes := make([]byte, 2*len(indices))
	for i, v := range indices {
		binary.LittleEndian.PutUint16(ibytes[2*i:], v)
	}
	vbuf, _ := device.CreateBuffer(&wgpu.BufferDescriptor{Size: uint64(len(vbytes)), Usage: gputypes.BufferUsageVertex | gputypes.BufferUsageCopyDst})
	defer vbuf.Release()
	q.WriteBuffer(vbuf, 0, vbytes)
	ibuf, _ := device.CreateBuffer(&wgpu.BufferDescriptor{Size: uint64((len(ibytes) + 3) &^ 3), Usage: gputypes.BufferUsageIndex | gputypes.BufferUsageCopyDst})
	defer ibuf.Release()
	q.WriteBuffer(ibuf, 0, append(ibytes, make([]byte, (4-len(ibytes)%4)%4)...))

	shader, err := device.CreateShaderModule(&wgpu.ShaderModuleDescriptor{WGSL: quadShader})
	if err != nil {
		t.Fatal(err)
	}
	defer shader.Release()
	pl, _ := device.CreatePipelineLayout(&wgpu.PipelineLayoutDescriptor{})
	defer pl.Release()
	const W, H = 16, 16
	pipeline, err := device.CreateRenderPipeline(&wgpu.RenderPipelineDescriptor{
		Layout: pl,
		Vertex: wgpu.VertexState{Module: shader, EntryPoint: "vs_main", Buffers: []gputypes.VertexBufferLayout{{
			ArrayStride: 8, StepMode: gputypes.VertexStepModeVertex,
			Attributes: []gputypes.VertexAttribute{{Format: gputypes.VertexFormatFloat32x2}},
		}}},
		Fragment: &wgpu.FragmentState{Module: shader, EntryPoint: "fs_main",
			Targets: []gputypes.ColorTargetState{{Format: gputypes.TextureFormatRGBA8Unorm, WriteMask: gputypes.ColorWriteMaskAll}}},
		Primitive:   gputypes.PrimitiveState{Topology: gputypes.PrimitiveTopologyTriangleList, CullMode: gputypes.CullModeNone},
		Multisample: gputypes.MultisampleState{Count: 1, Mask: 0xFFFFFFFF},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer pipeline.Release()
	tex, _ := device.CreateTexture(&wgpu.TextureDescriptor{
		Size: wgpu.Extent3D{Width: W, Height: H, DepthOrArrayLayers: 1}, MipLevelCount: 1, SampleCount: 1,
		Dimension: gputypes.TextureDimension2D, Format: gputypes.TextureFormatRGBA8Unorm,
		Usage: gputypes.TextureUsageRenderAttachment | gputypes.TextureUsageCopySrc,
	})
	defer tex.Release()
	view, _ := device.CreateTextureView(tex, nil)
	defer view.Release()

	enc, _ := device.CreateCommandEncoder(&wgpu.CommandEncoderDescriptor{})
	rp, _ := enc.BeginRenderPass(&wgpu.RenderPassDescriptor{ColorAttachments: []wgpu.RenderPassColorAttachment{{
		View: view, LoadOp: gputypes.LoadOpClear, StoreOp: gputypes.StoreOpStore, ClearValue: gputypes.Color{A: 1},
	}}})
	rp.SetPipeline(pipeline)
	rp.SetVertexBuffer(0, vbuf, 0)
	rp.SetIndexBuffer(ibuf, gputypes.IndexFormatUint16, 0)
	rp.DrawIndexed(gputypes.DrawIndexedArgs{IndexCount: 6, InstanceCount: 1, FirstIndex: firstIndex, BaseVertex: baseVertex})
	if err := rp.End(); err != nil {
		t.Fatal(err)
	}
	staging, _ := device.CreateBuffer(&wgpu.BufferDescriptor{Size: W * H * 4, Usage: gputypes.BufferUsageMapRead | gputypes.BufferUsageCopyDst})
	defer staging.Release()
	enc.CopyTextureToBuffer(tex, staging, []wgpu.BufferTextureCopy{{
		BufferLayout: wgpu.ImageDataLayout{BytesPerRow: W * 4, RowsPerImage: H},
		TextureBase:  wgpu.ImageCopyTexture{Texture: tex},
		Size:         wgpu.Extent3D{Width: W, Height: H, DepthOrArrayLayers: 1},
	}})
	cb, err := enc.Finish()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := q.Submit(cb); err != nil {
		t.Fatal(err)
	}
	if err := staging.Map(context.Background(), wgpu.MapModeRead, 0, W*H*4); err != nil {
		t.Fatal(err)
	}
	rng, _ := staging.MappedRange(0, W*H*4)
	px := rng.Bytes()
	filled := 0
	for i := 0; i < W*H; i++ {
		if px[4*i+1] > 128 && px[4*i] < 64 {
			filled++
		}
	}
	staging.Unmap()
	return filled
}
