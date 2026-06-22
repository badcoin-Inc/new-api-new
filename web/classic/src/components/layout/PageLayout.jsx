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

import HeaderBar from './headerbar';
import { Layout } from '@douyinfe/semi-ui';
import SiderBar from './SiderBar';
import App from '../../App';
import FooterBar from './Footer';
import { ToastContainer } from 'react-toastify';
import ErrorBoundary from '../common/ErrorBoundary';
import React, { useContext, useEffect, useRef, useState } from 'react';
import { useIsMobile } from '../../hooks/common/useIsMobile';
import { useSidebarCollapsed } from '../../hooks/common/useSidebarCollapsed';
import { useTranslation } from 'react-i18next';
import {
  API,
  getLogo,
  getSystemName,
  showError,
  setStatusData,
} from '../../helpers';
import { UserContext } from '../../context/User';
import { StatusContext } from '../../context/Status';
import { useLocation } from 'react-router-dom';
import { normalizeLanguage } from '../../i18n/language';
import { useActualTheme } from '../../context/Theme';
import StarNestBackground from './StarNestBackground';
const { Sider, Content, Header } = Layout;

const PageLayout = () => {
  const [userState, userDispatch] = useContext(UserContext);
  const [, statusDispatch] = useContext(StatusContext);
  const isMobile = useIsMobile();
  const [collapsed, , setCollapsed] = useSidebarCollapsed();
  const [drawerOpen, setDrawerOpen] = useState(false);
  const { i18n } = useTranslation();
  const location = useLocation();
  const actualTheme = useActualTheme();

  const cardProPages = [
    '/console/channel',
    '/console/log',
    '/console/redemption',
    '/console/user',
    '/console/token',
    '/console/midjourney',
    '/console/task',
    '/console/generation_jobs',
    '/console/models',
    '/pricing',
  ];

  const shouldHideFooter = cardProPages.includes(location.pathname);

  const shouldInnerPadding =
    location.pathname.includes('/console') &&
    !location.pathname.startsWith('/console/chat') &&
    location.pathname !== '/console/playground';

  const isConsoleRoute = location.pathname.startsWith('/console');
  const showSider = isConsoleRoute && (!isMobile || drawerOpen);
  const isNebulaMode = actualTheme === 'nebula';
  const isNebulaHome = isNebulaMode && location.pathname === '/';
  const isNebulaHeroRoute =
    isNebulaMode && ['/', '/login', '/register'].includes(location.pathname);
  const [hideNebulaHomeContent, setHideNebulaHomeContent] = useState(false);
  const nebulaHomeIdleTimerRef = useRef(null);
  const shouldStretchNebulaPanel = [
    '/console',
    '/console/personal',
    '/console/deployment',
    '/console/setting',
  ].includes(location.pathname);

  useEffect(() => {
    if (isMobile && drawerOpen && collapsed) {
      setCollapsed(false);
    }
  }, [isMobile, drawerOpen, collapsed, setCollapsed]);

  useEffect(() => {
    if (!isNebulaHome && hideNebulaHomeContent) {
      setHideNebulaHomeContent(false);
    }
  }, [isNebulaHome, hideNebulaHomeContent]);

  useEffect(() => {
    if (!isNebulaHome || isMobile) {
      if (nebulaHomeIdleTimerRef.current) {
        clearTimeout(nebulaHomeIdleTimerRef.current);
        nebulaHomeIdleTimerRef.current = null;
      }
      setHideNebulaHomeContent(false);
      return;
    }

    const resetNebulaHomeIdleTimer = () => {
      setHideNebulaHomeContent(false);
      if (nebulaHomeIdleTimerRef.current) {
        clearTimeout(nebulaHomeIdleTimerRef.current);
      }
      nebulaHomeIdleTimerRef.current = setTimeout(() => {
        setHideNebulaHomeContent(true);
      }, 10000);
    };

    resetNebulaHomeIdleTimer();
    window.addEventListener('mousemove', resetNebulaHomeIdleTimer);

    return () => {
      window.removeEventListener('mousemove', resetNebulaHomeIdleTimer);
      if (nebulaHomeIdleTimerRef.current) {
        clearTimeout(nebulaHomeIdleTimerRef.current);
        nebulaHomeIdleTimerRef.current = null;
      }
    };
  }, [isNebulaHome, isMobile]);

  const nebulaHomeFadeClass =
    isNebulaHome && hideNebulaHomeContent
      ? 'nebula-home-content-fade-target nebula-home-content-hidden'
      : isNebulaHome
        ? 'nebula-home-content-fade-target'
        : undefined;

  const loadUser = () => {
    let user = localStorage.getItem('user');
    if (user) {
      let data = JSON.parse(user);
      userDispatch({ type: 'login', payload: data });
    }
  };

  const loadStatus = async () => {
    try {
      const res = await API.get('/api/status');
      const { success, data } = res.data;
      if (success) {
        statusDispatch({ type: 'set', payload: data });
        setStatusData(data);
      } else {
        showError('Unable to connect to server');
      }
    } catch (error) {
      showError('Failed to load status');
    }
  };

  useEffect(() => {
    loadUser();
    loadStatus().catch(console.error);
    let systemName = getSystemName();
    if (systemName) {
      document.title = systemName;
    }
    let logo = getLogo();
    if (logo) {
      let linkElement = document.querySelector("link[rel~='icon']");
      if (linkElement) {
        linkElement.href = logo;
      }
    }
  }, []);

  useEffect(() => {
    let preferredLang;

    if (userState?.user?.setting) {
      try {
        const settings = JSON.parse(userState.user.setting);
        preferredLang = normalizeLanguage(settings.language);
      } catch (e) {
        // Ignore parse errors
      }
    }

    if (!preferredLang) {
      const savedLang = localStorage.getItem('i18nextLng');
      if (savedLang) {
        preferredLang = normalizeLanguage(savedLang);
      }
    }

    if (preferredLang) {
      localStorage.setItem('i18nextLng', preferredLang);
      if (preferredLang !== i18n.language) {
        i18n.changeLanguage(preferredLang);
      }
    }
  }, [i18n, userState?.user?.setting]);

  return (
    <Layout
      className='app-layout'
      style={{
        display: 'flex',
        flexDirection: 'column',
        overflow: isMobile ? 'visible' : 'hidden',
      }}
    >
      {isNebulaMode && (
        <StarNestBackground
          interactive={isNebulaHeroRoute && !isMobile}
          forceLowPower={!isNebulaHeroRoute}
        />
      )}
      <Header
        className={nebulaHomeFadeClass}
        style={{
          padding: 0,
          height: 'auto',
          lineHeight: 'normal',
          position: 'fixed',
          width: '100%',
          top: 0,
          zIndex: 100,
        }}
      >
        <HeaderBar
          onMobileMenuToggle={() => setDrawerOpen((prev) => !prev)}
          drawerOpen={drawerOpen}
          hideNebulaHomeContent={hideNebulaHomeContent}
        />
      </Header>
      <Layout
        style={{
          overflow: isMobile ? 'visible' : 'auto',
          display: 'flex',
          flexDirection: 'column',
          marginTop: '64px',
        }}
      >
        {showSider && (
          <Sider
            className='app-sider'
            style={{
              position: 'fixed',
              left: 0,
              top: '64px',
              zIndex: 99,
              border: 'none',
              paddingRight: '0',
              width: 'var(--sidebar-current-width)',
            }}
          >
            <SiderBar
              onNavigate={() => {
                if (isMobile) setDrawerOpen(false);
              }}
            />
          </Sider>
        )}
        <Layout
          style={{
            marginLeft: isMobile
              ? '0'
              : showSider
                ? 'var(--sidebar-current-width)'
                : '0',
            flex: '1 1 auto',
            display: 'flex',
            flexDirection: 'column',
            minHeight: 0,
          }}
        >
          <Content
            className={
              isNebulaHome
                ? nebulaHomeFadeClass
                : isNebulaMode && isConsoleRoute
                  ? `nebula-console-panel${shouldStretchNebulaPanel ? ' nebula-console-panel-stretch' : ''}`
                  : undefined
            }
            style={{
              flex: '1 0 auto',
              overflowY: isMobile || isNebulaMode ? 'visible' : 'hidden',
              WebkitOverflowScrolling: 'touch',
              padding: shouldInnerPadding ? (isMobile ? '5px' : '24px') : '0',
              position: 'relative',
              zIndex: isNebulaMode ? 1 : undefined,
            }}
          >
            <ErrorBoundary>
              <App />
            </ErrorBoundary>
          </Content>
          {!shouldHideFooter && (
            <Layout.Footer
              className={
                isNebulaMode
                  ? `nebula-footer${isNebulaHome ? ` nebula-home-footer ${nebulaHomeFadeClass}` : ''}`
                  : undefined
              }
              style={{
                flex: '0 0 auto',
                width: '100%',
              }}
            >
              <FooterBar />
            </Layout.Footer>
          )}
        </Layout>
      </Layout>
      <ToastContainer />
    </Layout>
  );
};

export default PageLayout;
