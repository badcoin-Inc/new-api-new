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
import { Link } from '@tanstack/react-router'
import { BookOpen, KeyRound } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { useStatus } from '@/hooks/use-status'
import { cn } from '@/lib/utils'

type NebulaHomeProps = {
  contentHidden: boolean
}

export function NebulaHome(props: NebulaHomeProps) {
  const { t } = useTranslation()
  const { status } = useStatus()
  const docsUrl =
    (status?.docs_link as string | undefined) || 'https://docs.newapi.pro'

  return (
    <main
      className={cn(
        'nebula-home nebula-home-fade-target',
        props.contentHidden && 'nebula-home-content-hidden'
      )}
    >
      <section className='nebula-home-hero'>
        <div className='nebula-home-inner'>
          <div className='nebula-home-content'>
            <p className='nebula-home-eyebrow'>OpenAI Compatible API</p>
            <h1>OpenKaya</h1>
            <p className='nebula-home-description'>
              {t(
                'Connect diverse intelligence and build your unified AI gateway'
              )}
            </p>
            <div className='mt-8 flex flex-wrap items-center gap-4'>
              <Button
                size='lg'
                className='nebula-home-button rounded-full px-8'
                render={<Link to='/dashboard' />}
              >
                <KeyRound className='size-4' />
                {t('Get API key')}
              </Button>
              <Button
                size='lg'
                variant='outline'
                className='nebula-home-button rounded-full px-6'
                render={
                  <a href={docsUrl} target='_blank' rel='noopener noreferrer' />
                }
              >
                <BookOpen className='size-4' />
                {t('Docs')}
              </Button>
            </div>
          </div>
        </div>
      </section>
    </main>
  )
}
