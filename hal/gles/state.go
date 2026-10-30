// Copyright 2025 The GoGPU Authors
// SPDX-License-Identifier: MIT

//go:build (windows || linux) && !(js && wasm)

package gles

import (
	"os"
	"strings"

	"github.com/gogpu/gputypes"
	"github.com/gogpu/wgpu/hal/gles/gl"
)

// glState is what the commands of the current Submit have set so far, so a
// command that would set the same again is skipped. On some drivers (Windows
// 7's AMD) every GL call costs tens of microseconds, and a draw sets some
// forty states, most of them as the draw before. Submit resets it: the GL
// state changed outside (buffer and texture writes, the present) is not
// tracked, and neither are deleted objects.
type glState struct {
	program    uint32
	hasProgram bool
	pipe       *SetPipelineStateCommand // the last applied (same compares by value)
	vb         vbKey                    // the vertex buffer the attributes point into...
	vbBase     int32                    // ...and how many vertices on the bound one begins (see SetVertexBufferCommand)
	vbCmd      *SetVertexBufferCommand
	hasVB      bool
	multiSlot  bool // a vertex buffer slot other than 0 was set: no vbBase
	ib         uint32
	hasIB      bool
	vp         SetViewportCommand
	hasVP      bool
	sc         SetScissorCommand
	hasSC      bool
	active     uint32     // the active texture unit + 1 (0: unknown)
	tex        [32]uint32 // the texture bound to each unit + 1 (0: unknown)
	samp       [32]uint32 // the sampler bound to each unit + 1
	ubo        [32]uboBinding
}

type vbKey struct {
	id     uint32
	offset uint64
	layout *gputypes.VertexBufferLayout
}

type uboBinding struct {
	target, id uint32
	off, size  int
	set        bool
}

var st glState

func resetState() { st = glState{} }

// GOGPU_GL_NOCACHE names the parts of the cache to switch off (for finding a
// cache bug): "all", or any of prog, pipe, vb, ib, vp, sc, tex, samp, ubo.
var noCache = os.Getenv("GOGPU_GL_NOCACHE")

// forget drops the parts GOGPU_GL_NOCACHE names, before each command.
func forget() {
	if noCache == "" {
		return
	}
	off := func(what string) bool { return noCache == "all" || strings.Contains(noCache, what) }
	if off("prog") {
		st.hasProgram = false
	}
	if off("pipe") {
		st.pipe = nil
	}
	if off("vb") {
		st.hasVB, st.vbBase = false, 0
	}
	if off("ib") {
		st.hasIB = false
	}
	if off("vp") {
		st.hasVP = false
	}
	if off("sc") {
		st.hasSC = false
	}
	if off("tex") {
		st.forgetTextures()
	}
	if off("samp") {
		st.samp = [32]uint32{}
	}
	if off("ubo") {
		st.ubo = [32]uboBinding{}
	}
}

// forgetTextures is called by the commands that bind textures on the active unit themselves.
func (s *glState) forgetTextures() {
	s.active, s.tex = 0, [32]uint32{}
}

// activeTexture selects unit, unless it is selected.
func activeTexture(ctx *gl.Context, unit uint32) {
	if st.active == unit+1 {
		return
	}
	st.active = unit + 1
	ctx.ActiveTexture(gl.TEXTURE0 + unit)
}

// untrackTexture forgets what the active unit has bound: a copy into a texture
// bound it there (and then 0) between draws, so a draw of the texture the cache
// still names would skip its bind and sample nothing.
func untrackTexture() {
	if st.active == 0 {
		st.tex = [32]uint32{}
		return
	}
	st.tex[st.active-1] = 0
}

// same reports whether c sets exactly the state o did.
func (c *SetPipelineStateCommand) same(o *SetPipelineStateCommand) bool {
	if c.cullMode != o.cullMode || c.frontFace != o.frontFace || c.stencilRef != o.stencilRef || len(c.colorTargets) != len(o.colorTargets) {
		return false
	}
	if (c.depthStencil == nil) != (o.depthStencil == nil) || c.depthStencil != nil && *c.depthStencil != *o.depthStencil {
		return false
	}
	for i := range c.colorTargets {
		a, b := c.colorTargets[i], o.colorTargets[i]
		if a.WriteMask != b.WriteMask || (a.Blend == nil) != (b.Blend == nil) || a.Blend != nil && *a.Blend != *b.Blend {
			return false
		}
	}
	return true
}
