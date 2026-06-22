/*
Copyright (C) 2025 QuantumNous

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

import React, { useEffect, useRef } from 'react';

const vertexShaderSource = `
attribute vec2 position;

void main() {
  gl_Position = vec4(position, 0.0, 1.0);
}
`;

const getFragmentShaderSource = ({ lowPower }) => `
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

  float s = 0.1;
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
  vec3 darkColor = v * exposure;

  fragColor = vec4(darkColor, 1.);
}

void main() {
  mainImage(gl_FragColor, gl_FragCoord.xy);
}
`;

function createShader(gl, type, source) {
  const shader = gl.createShader(type);
  gl.shaderSource(shader, source);
  gl.compileShader(shader);

  if (!gl.getShaderParameter(shader, gl.COMPILE_STATUS)) {
    const info = gl.getShaderInfoLog(shader);
    gl.deleteShader(shader);
    throw new Error(`Shader failed to compile: ${info}`);
  }

  return shader;
}

function createProgram(gl, lowPower) {
  const vertexShader = createShader(gl, gl.VERTEX_SHADER, vertexShaderSource);
  const fragmentShader = createShader(
    gl,
    gl.FRAGMENT_SHADER,
    getFragmentShaderSource({ lowPower }),
  );
  const shaderProgram = gl.createProgram();

  gl.attachShader(shaderProgram, vertexShader);
  gl.attachShader(shaderProgram, fragmentShader);
  gl.linkProgram(shaderProgram);

  gl.deleteShader(vertexShader);
  gl.deleteShader(fragmentShader);

  if (!gl.getProgramParameter(shaderProgram, gl.LINK_STATUS)) {
    const info = gl.getProgramInfoLog(shaderProgram);
    gl.deleteProgram(shaderProgram);
    throw new Error(`Shader program failed to link: ${info}`);
  }

  return shaderProgram;
}

function shouldUseLowPowerMode() {
  const lowCoreCount =
    typeof navigator.hardwareConcurrency === 'number' &&
    navigator.hardwareConcurrency <= 4;
  const lowMemory =
    typeof navigator.deviceMemory === 'number' && navigator.deviceMemory <= 4;
  const mobileDevice = /Android|iPhone|iPad|iPod|Mobile/i.test(
    navigator.userAgent,
  );

  return mobileDevice || lowCoreCount || lowMemory;
}

function getWebGLRenderer(gl) {
  const debugInfo = gl.getExtension('WEBGL_debug_renderer_info');
  return debugInfo ? gl.getParameter(debugInfo.UNMASKED_RENDERER_WEBGL) : '';
}

function shouldUseUltraLowPowerMode(gl) {
  const renderer = getWebGLRenderer(gl).toLowerCase();
  const softwareRenderer = /swiftshader|llvmpipe|software|mesa offscreen/.test(
    renderer,
  );

  return softwareRenderer;
}

const StarNestBackground = ({ interactive, forceLowPower = false }) => {
  const canvasRef = useRef(null);

  useEffect(() => {
    const canvas = canvasRef.current;
    if (!canvas) return undefined;

    const gl = canvas.getContext('webgl', { antialias: false, alpha: false });
    if (!gl) {
      canvas.classList.add('star-nest-unavailable');
      return undefined;
    }

    const ultraLowPowerMode = shouldUseUltraLowPowerMode(gl);
    const lowPowerMode =
      forceLowPower || shouldUseLowPowerMode() || ultraLowPowerMode;
    let program;
    try {
      program = createProgram(gl, lowPowerMode);
    } catch (error) {
      console.warn('[StarNestBackground] WebGL background disabled', error);
      canvas.classList.add('star-nest-unavailable');
      return undefined;
    }
    const positionLocation = gl.getAttribLocation(program, 'position');
    const resolutionLocation = gl.getUniformLocation(program, 'iResolution');
    const timeLocation = gl.getUniformLocation(program, 'iTime');
    const mouseLocation = gl.getUniformLocation(program, 'iMouse');
    const mouse = {
      x: canvas.clientWidth * 0.5,
      y: canvas.clientHeight * 0.5,
      downX: 0,
      downY: 0,
    };
    const startTime = performance.now();
    let lastRenderTime = 0;
    let frameId;

    const buffer = gl.createBuffer();
    gl.bindBuffer(gl.ARRAY_BUFFER, buffer);
    gl.bufferData(
      gl.ARRAY_BUFFER,
      new Float32Array([-1, -1, 3, -1, -1, 3]),
      gl.STATIC_DRAW,
    );

    gl.useProgram(program);
    gl.enableVertexAttribArray(positionLocation);
    gl.vertexAttribPointer(positionLocation, 2, gl.FLOAT, false, 0, 0);

    const handlePointerMove = (event) => {
      const rect = canvas.getBoundingClientRect();
      mouse.x = event.clientX - rect.left;
      mouse.y = rect.height - (event.clientY - rect.top);
    };

    const resizeCanvas = () => {
      const pixelRatio = ultraLowPowerMode
        ? 0.75
        : lowPowerMode
          ? Math.min(window.devicePixelRatio || 1, 1)
          : Math.min(window.devicePixelRatio || 1, 2);
      const width = Math.floor(canvas.clientWidth * pixelRatio);
      const height = Math.floor(canvas.clientHeight * pixelRatio);

      if (canvas.width === width && canvas.height === height) return;

      canvas.width = width;
      canvas.height = height;
      gl.viewport(0, 0, width, height);
      if (!interactive || ultraLowPowerMode) {
        mouse.x = width * 0.5;
        mouse.y = height * 0.52;
      }
    };

    const render = (now) => {
      if (document.hidden) {
        frameId = requestAnimationFrame(render);
        return;
      }

      const frameInterval = ultraLowPowerMode ? 84 : lowPowerMode ? 42 : 33;
      if (frameInterval > 0 && now - lastRenderTime < frameInterval) {
        frameId = requestAnimationFrame(render);
        return;
      }

      lastRenderTime = now;
      resizeCanvas();

      gl.uniform3f(resolutionLocation, canvas.width, canvas.height, 1);
      gl.uniform1f(timeLocation, (now - startTime) / 1000);
      gl.uniform4f(mouseLocation, mouse.x, mouse.y, mouse.downX, mouse.downY);
      gl.drawArrays(gl.TRIANGLES, 0, 3);

      frameId = requestAnimationFrame(render);
    };

    if (interactive && !ultraLowPowerMode) {
      window.addEventListener('pointermove', handlePointerMove);
    }

    frameId = requestAnimationFrame(render);

    return () => {
      if (frameId) {
        cancelAnimationFrame(frameId);
      }
      window.removeEventListener('pointermove', handlePointerMove);
      gl.deleteBuffer(buffer);
      gl.deleteProgram(program);
    };
  }, [forceLowPower, interactive]);

  return <canvas ref={canvasRef} className='star-nest-canvas' aria-hidden />;
};

export default StarNestBackground;
