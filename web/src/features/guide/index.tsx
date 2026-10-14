import { Link, type LinkProps } from '@tanstack/react-router'
import {
  ArrowRight,
  Camera,
  CloudUpload,
  HardDrive,
  Image as ImageIcon,
  KeyRound,
  Link2,
  Server,
  ShieldAlert,
  Terminal,
  TriangleAlert,
} from 'lucide-react'
import { useMessages } from '@/context/locale-provider'
import { Page } from '@/components/layout/page'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { CodeBlock, CopyValue } from './code-block'
import { KeyValueList, Note, Section } from './section'

/** 错误码速查表的行,与服务端上传错误映射一一对应。 */
const ERROR_CODES = [400, 401, 403, 413, 415, 422, 429, 503] as const

/** 示例里的占位短 ID。 */
const SAMPLE_ID = 'R0RjUyP1UW'

export function Guide() {
  const m = useMessages()
  const origin = window.location.origin
  const curlExample = [
    'curl -H "Authorization: Bearer <token>" \\',
    '  -F "file=@photo.png" \\',
    `  ${origin}/api/upload`,
  ].join('\n')
  const responseExample = `{\n  "id": "${SAMPLE_ID}",\n  "url": "${origin}/i/${SAMPLE_ID}"\n}`
  const urlFetchExample = [
    'curl -H "Authorization: Bearer <token>" \\',
    '  -H "Content-Type: application/json" \\',
    `  -d '{"url":"https://example.com/photo.png"}' \\`,
    `  ${origin}/api/upload/url`,
  ].join('\n')
  const accessExample = [
    `${origin}/i/${SAMPLE_ID}`,
    `${origin}/i/${SAMPLE_ID}?w=400`,
    `${origin}/i/${SAMPLE_ID}?w=400&format=webp`,
  ].join('\n')

  return (
    <Page title={m.guide.title} description={m.guide.description}>
      <Section no='01' icon={CloudUpload} title={m.guide.sections.quickStart}>
        <div className='grid gap-3 md:grid-cols-3'>
          <Step
            n={1}
            icon={HardDrive}
            title={m.guide.step1Title}
            desc={m.guide.step1Desc}
            to='/channels'
            linkLabel={m.guide.goTo.channels}
          />
          <Step
            n={2}
            icon={KeyRound}
            title={m.guide.step2Title}
            desc={m.guide.step2Desc}
            to='/tokens'
            linkLabel={m.guide.goTo.tokens}
          />
          <Step
            n={3}
            icon={ImageIcon}
            title={m.guide.step3Title}
            desc={m.guide.step3Desc}
            to='/images'
            linkLabel={m.guide.goTo.images}
          />
        </div>
      </Section>

      <div className='grid gap-6 xl:grid-cols-2'>
        <Section no='02' icon={Terminal} title={m.guide.sections.curl}>
          <Note>{m.guide.curlIntro}</Note>
          <CodeBlock code={curlExample} />
          <KeyValueList
            items={[
              { key: 'file', value: m.guide.fields.file },
              { key: 'ttl', value: m.guide.fields.ttl },
              { key: 'exif_policy', value: m.guide.fields.exifPolicy },
              { key: 'Idempotency-Key', value: m.guide.fields.idempotencyKey },
            ]}
          />
          <div className='space-y-2'>
            <Note>{m.guide.response}</Note>
            <CodeBlock code={responseExample} />
          </div>
        </Section>

        <Section no='03' icon={Camera} title={m.guide.sections.picgo}>
          <Note>{m.guide.picgoIntro}</Note>
          <KeyValueList
            items={[
              {
                key: m.guide.picgo.apiUrl,
                value: <CopyValue value={`${origin}/api/upload`} />,
              },
              { key: m.guide.picgo.method, value: <CopyValue value='POST' /> },
              {
                key: m.guide.picgo.fileField,
                value: <CopyValue value='file' />,
              },
              {
                key: m.guide.picgo.headers,
                value: (
                  <CopyValue value='{"Authorization": "Bearer <token>"}' />
                ),
              },
              { key: m.guide.picgo.jsonPath, value: <CopyValue value='url' /> },
            ]}
          />
          <Alert>
            <ShieldAlert />
            <AlertDescription>{m.guide.baseUrlNote}</AlertDescription>
          </Alert>
        </Section>

        <Section
          no='04'
          icon={Server}
          title={m.guide.sections.s3}
          action={{ to: '/tokens', label: m.guide.goTo.tokens }}
        >
          <Note>{m.guide.s3Intro}</Note>
          <KeyValueList
            items={[
              {
                key: m.guide.s3.credentials,
                value: m.guide.s3.credentialsDesc,
              },
              { key: m.guide.s3.bucket, value: m.guide.s3.bucketDesc },
              { key: m.guide.s3.style, value: m.guide.s3.styleDesc },
              { key: m.guide.s3.urls, value: m.guide.s3.urlsDesc },
              { key: m.guide.s3.ops, value: m.guide.s3.opsDesc },
            ]}
          />
          <Note>{m.guide.s3Note}</Note>
        </Section>

        <Section no='05' icon={Link2} title={m.guide.sections.urlFetch}>
          <Note>{m.guide.urlFetchIntro}</Note>
          <CodeBlock code={urlFetchExample} />
          <KeyValueList
            items={[
              { key: 'url', value: m.guide.urlFetchFields.url },
              { key: 'file_name', value: m.guide.urlFetchFields.fileName },
              {
                key: 'ttl / exif_policy',
                value: m.guide.urlFetchFields.ttlAndExif,
              },
            ]}
          />
          <Alert className='border-warning/40 bg-warning/10'>
            <TriangleAlert className='text-warning' />
            <AlertTitle>{m.guide.urlFetchWarnTitle}</AlertTitle>
            <AlertDescription>{m.guide.urlFetchWarnDesc}</AlertDescription>
          </Alert>
        </Section>
      </div>

      <Section no='06' icon={ImageIcon} title={m.guide.sections.access}>
        <Note>{m.guide.accessIntro}</Note>
        <CodeBlock code={accessExample} />
        <KeyValueList
          items={[
            {
              key: m.guide.access.shortLink,
              value: m.guide.access.shortLinkDesc,
            },
            { key: m.guide.access.alias, value: m.guide.access.aliasDesc },
            { key: m.guide.access.width, value: m.guide.access.widthDesc },
            { key: m.guide.access.format, value: m.guide.access.formatDesc },
          ]}
        />
      </Section>

      <Section
        no='07'
        icon={ImageIcon}
        title={m.guide.sections.webUpload}
        action={{ to: '/images', label: m.guide.goTo.images }}
      >
        <Note>{m.guide.webUploadIntro}</Note>
      </Section>

      <Section no='08' icon={ShieldAlert} title={m.guide.sections.errors}>
        <div className='-mx-6 overflow-x-auto px-6'>
          <Table className='min-w-[36rem]'>
            <TableHeader>
              <TableRow>
                <TableHead className='w-24'>{m.guide.errorCols.code}</TableHead>
                <TableHead>{m.guide.errorCols.meaning}</TableHead>
                <TableHead className='w-72'>
                  {m.guide.errorCols.advice}
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {ERROR_CODES.map((code) => (
                <TableRow key={code}>
                  <TableCell>
                    <Badge
                      variant='outline'
                      className={
                        code >= 500
                          ? 'border-destructive/50 text-destructive'
                          : 'border-warning/50 text-warning'
                      }
                    >
                      {code}
                    </Badge>
                  </TableCell>
                  <TableCell className='whitespace-normal'>
                    {m.guide.errorRows[code].meaning}
                  </TableCell>
                  <TableCell className='whitespace-normal text-muted-foreground'>
                    {m.guide.errorRows[code].advice}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      </Section>
    </Page>
  )
}

function Step({
  n,
  icon: Icon,
  title,
  desc,
  to,
  linkLabel,
}: {
  n: number
  icon: React.ElementType
  title: string
  desc: string
  to: LinkProps['to']
  linkLabel: string
}) {
  return (
    <div className='flex flex-col gap-3 rounded-lg border bg-muted/30 p-4'>
      <div className='flex items-center gap-3'>
        <span className='flex size-7 items-center justify-center rounded-full bg-primary font-mono text-xs font-semibold text-primary-foreground'>
          {n}
        </span>
        <Icon className='size-4 text-primary' />
        <span className='font-medium'>{title}</span>
      </div>
      <p className='flex-1 text-sm leading-relaxed text-muted-foreground'>
        {desc}
      </p>
      <Link
        to={to}
        className='inline-flex items-center gap-1 text-sm font-medium text-primary underline-offset-4 hover:underline'
      >
        {linkLabel}
        <ArrowRight className='size-3.5' />
      </Link>
    </div>
  )
}
