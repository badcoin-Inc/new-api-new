/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { useEffect, useRef } from 'react'

const VERTEX_SHADER = `
attribute vec2 position;
void main() { gl_Position = vec4(position, 0.0, 1.0); }
`

function getFragmentShader(lowPower: boolean) {
  return `
precision highp float;
uniform vec3 iResolution;
uniform float iTime;
uniform vec4 iMouse;

#define iterations ${lowPower ? 10 : 17}
#define formuparam 0.53
#define volsteps ${lowPower ? 12 : 20}
#define stepsize ${lowPower ? '0.14' : '0.1'}
#define zoom 0.800
#define tile 0.850
#define speed ${lowPower ? '0.002' : '0.003'}
#define brightness ${lowPower ? '0.0018' : '0.0015'}
#define darkmatter 0.300
#define distfading ${lowPower ? '0.760' : '0.730'}
#define saturation 0.850
#define exposure ${lowPower ? '0.016' : '0.010'}

void mainImage(out vec4 fragColor, in vec2 fragCoord) {
  vec2 uv = fragCoord.xy / iResolution.xy - .5;
  uv.y *= iResolution.y / iResolution.x;
  vec3 dir = vec3(uv * zoom, 1.);
  float time = iTime * speed + .25;
  float a1 = .5 + iMouse.x / iResolution.x * .12;
  float a2 = .8 + iMouse.y / iResolution.y * .12;
  mat2 rot1 = mat2(cos(a1), sin(a1), -sin(a1), cos(a1));
  mat2 rot2 = mat2(cos(a2), sin(a2), -sin(a2), cos(a2));
  dir.xz *= rot1;
  dir.xy *= rot2;

  vec3 from = vec3(1., .5, .5);
  from += vec3(time * 2., time, -2.);
  from.xz *= rot1;
  from.xy *= rot2;

  float s = .1;
  float fade = 1.;
  vec3 v = vec3(0.);
  for (int r = 0; r < volsteps; r++) {
    vec3 p = from + s * dir * .5;
    p = abs(vec3(tile) - mod(p, vec3(tile * 2.)));
    float pa;
    float a = pa = 0.;
    for (int i = 0; i < iterations; i++) {
      p = abs(p) / dot(p, p) - formuparam;
      a += abs(length(p) - pa);
      pa = length(p);
    }
    float dm = max(0., darkmatter - a * a * .001);
    a *= a * a;
    if (r > 6) fade *= 1. - dm;
    v += fade;
    v += vec3(s, s * s, s * s * s * s) * a * brightness * fade;
    fade *= distfading;
    s += stepsize;
  }
  v = mix(vec3(length(v)), v, saturation);
  fragColor = vec4(v * exposure, 1.);
}

void main() { mainImage(gl_FragColor, gl_FragCoord.xy); }
`
}

function compileShader(
  gl: WebGLRenderingContext,
  type: number,
  source: string
) {
  const shader = gl.createShader(type)
  if (!shader) throw new Error('Unable to create WebGL shader')
  gl.shaderSource(shader, source)
  gl.compileShader(shader)
  if (!gl.getShaderParameter(shader, gl.COMPILE_STATUS)) {
    const message = gl.getShaderInfoLog(shader)
    gl.deleteShader(shader)
    throw new Error(`Shader failed to compile: ${message}`)
  }
  return shader
}

function createProgram(gl: WebGLRenderingContext, lowPower: boolean) {
  const vertex = compileShader(gl, gl.VERTEX_SHADER, VERTEX_SHADER)
  const fragment = compileShader(
    gl,
    gl.FRAGMENT_SHADER,
    getFragmentShader(lowPower)
  )
  const program = gl.createProgram()
  if (!program) throw new Error('Unable to create WebGL program')
  gl.attachShader(program, vertex)
  gl.attachShader(program, fragment)
  gl.linkProgram(program)
  gl.deleteShader(vertex)
  gl.deleteShader(fragment)
  if (!gl.getProgramParameter(program, gl.LINK_STATUS)) {
    const message = gl.getProgramInfoLog(program)
    gl.deleteProgram(program)
    throw new Error(`Shader program failed to link: ${message}`)
  }
  return program
}

function shouldUseLowPowerMode() {
  const lowCoreCount = (navigator.hardwareConcurrency ?? 8) <= 4
  const deviceMemory = (navigator as Navigator & { deviceMemory?: number })
    .deviceMemory
  const lowMemory = typeof deviceMemory === 'number' && deviceMemory <= 4
  const mobile = window.matchMedia('(max-width: 767px)').matches
  return lowCoreCount || lowMemory || mobile
}

type NebulaBackgroundProps = {
  forceLowPower?: boolean
  interactive?: boolean
}

export default function NebulaBackground(props: NebulaBackgroundProps) {
  const canvasRef = useRef<HTMLCanvasElement>(null)

  useEffect(() => {
    const canvas = canvasRef.current
    if (!canvas) return

    const gl = canvas.getContext('webgl', { alpha: false, antialias: false })
    if (!gl) {
      canvas.classList.add('nebula-background-unavailable')
      return
    }

    const lowPower = props.forceLowPower || shouldUseLowPowerMode()
    let program: WebGLProgram
    try {
      program = createProgram(gl, lowPower)
    } catch {
      canvas.classList.add('nebula-background-unavailable')
      return
    }

    const position = gl.getAttribLocation(program, 'position')
    const resolution = gl.getUniformLocation(program, 'iResolution')
    const time = gl.getUniformLocation(program, 'iTime')
    const mouseUniform = gl.getUniformLocation(program, 'iMouse')
    const buffer = gl.createBuffer()
    const mouse = { x: 0, y: 0 }
    const startTime = performance.now()
    let frameId = 0
    let lastFrame = 0

    gl.bindBuffer(gl.ARRAY_BUFFER, buffer)
    gl.bufferData(
      gl.ARRAY_BUFFER,
      new Float32Array([-1, -1, 3, -1, -1, 3]),
      gl.STATIC_DRAW
    )
    gl.useProgram(program)
    gl.enableVertexAttribArray(position)
    gl.vertexAttribPointer(position, 2, gl.FLOAT, false, 0, 0)

    const resize = () => {
      const ratio = Math.min(window.devicePixelRatio || 1, lowPower ? 1 : 2)
      const width = Math.max(1, Math.floor(canvas.clientWidth * ratio))
      const height = Math.max(1, Math.floor(canvas.clientHeight * ratio))
      if (canvas.width === width && canvas.height === height) return
      canvas.width = width
      canvas.height = height
      gl.viewport(0, 0, width, height)
      mouse.x = width * 0.5
      mouse.y = height * 0.52
    }

    const onPointerMove = (event: PointerEvent) => {
      if (!props.interactive || lowPower) return
      const rect = canvas.getBoundingClientRect()
      mouse.x =
        rect.width * 0.5 + (event.clientX - rect.left - rect.width * 0.5) * 0.5
      mouse.y =
        rect.height * 0.5 +
        (rect.height - event.clientY + rect.top - rect.height * 0.5) * 0.5
    }

    const render = (now: number) => {
      frameId = window.requestAnimationFrame(render)
      if (document.hidden || now - lastFrame < (lowPower ? 42 : 16)) return
      lastFrame = now
      resize()
      gl.uniform3f(resolution, canvas.width, canvas.height, 1)
      gl.uniform1f(time, (now - startTime) / 1000)
      gl.uniform4f(mouseUniform, mouse.x, mouse.y, 0, 0)
      gl.drawArrays(gl.TRIANGLES, 0, 3)
    }

    if (props.interactive && !lowPower) {
      window.addEventListener('pointermove', onPointerMove, { passive: true })
    }
    frameId = window.requestAnimationFrame(render)

    return () => {
      window.removeEventListener('pointermove', onPointerMove)
      window.cancelAnimationFrame(frameId)
      gl.deleteBuffer(buffer)
      gl.deleteProgram(program)
    }
  }, [props.forceLowPower, props.interactive])

  return (
    <canvas ref={canvasRef} className='nebula-background' aria-hidden='true' />
  )
}
